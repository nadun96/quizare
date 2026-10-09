package storage

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/platform/db"
)

// A backup (PL-FR-07, PL-FR-09, D-57) is the whole database read from one
// consistent snapshot, as plain COPY text per table, gzipped and encrypted
// with age under the admin's passphrase:
//
//	QPBACKUP 1
//	{manifest: migrations, tables and columns, sequences, master-key id}
//	TABLE auth.users
//	<COPY text rows>
//	\.
//	… one section per table …
//	END {"rows": {"auth.users": 12, …}}
//
// It needs no pg_dump, so it works in the distroless container too, and it
// restores with `server restore-backup`. The job queue (River's tables) and
// the backup history itself are left out.

const magic = "QPBACKUP 1"

// Manifest describes a backup's contents.
type Manifest struct {
	CreatedAt  time.Time         `json:"created_at"`
	Migrations []string          `json:"migrations"`
	Tables     []TableSpec       `json:"tables"`
	Sequences  map[string]*int64 `json:"sequences"`
	// KEKID identifies the master key the stored AI keys are encrypted
	// with, so a restore can say whether the right key file is in place.
	KEKID string `json:"kek_id,omitempty"`
}

type TableSpec struct {
	Name    string   `json:"name"` // schema.table
	Columns []string `json:"columns"`
}

type trailer struct {
	Rows map[string]int64 `json:"rows"`
}

// Only the app's own schemas; public holds River's queue and the migration list.
const appSchemas = `n.nspname NOT IN ('pg_catalog', 'information_schema', 'public') AND n.nspname NOT LIKE 'pg\_%'`

// skipTables are not backed up: the history of backups and their links
// describe files on this server, not data to move.
var skipTables = map[string]bool{"storage.backups": true, "storage.download_links": true}

func ident(qualified string) string {
	return pgx.Identifier(strings.SplitN(qualified, ".", 2)).Sanitize()
}

func quoteCols(cols []string) string {
	q := make([]string, len(cols))
	for i, c := range cols {
		q[i] = pgx.Identifier{c}.Sanitize()
	}
	return strings.Join(q, ", ")
}

// Progress reports how far a backup got.
type Progress func(tablesDone, tablesTotal int, rows int64)

// dump writes the plain backup stream of the database conn is connected to.
// It reads in one REPEATABLE READ, READ ONLY transaction: a consistent
// snapshot that takes no locks beyond those every query takes, so classes
// carry on writing while it runs (PL-NFR-01).
func dump(ctx context.Context, conn *pgx.Conn, w io.Writer, kekID string, progress Progress) (Manifest, error) {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Manifest{}, err
	}
	defer tx.Rollback(ctx)
	m := Manifest{CreatedAt: time.Now().UTC(), KEKID: kekID, Sequences: map[string]*int64{}}
	rows, _ := tx.Query(ctx, `SELECT version FROM public.schema_migrations ORDER BY version`)
	if m.Migrations, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return m, err
	}
	rows, _ = tx.Query(ctx, `SELECT n.nspname || '.' || c.relname,
			array_agg(a.attname::text ORDER BY a.attnum)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped AND a.attgenerated = ''
		WHERE c.relkind = 'r' AND `+appSchemas+`
		GROUP BY 1 ORDER BY 1`)
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (TableSpec, error) {
		var t TableSpec
		err := r.Scan(&t.Name, &t.Columns)
		return t, err
	})
	if err != nil {
		return m, err
	}
	for _, t := range all {
		if !skipTables[t.Name] {
			m.Tables = append(m.Tables, t)
		}
	}
	rows, _ = tx.Query(ctx, `SELECT s.schemaname || '.' || s.sequencename, s.last_value FROM pg_sequences s
		JOIN pg_namespace n ON n.nspname = s.schemaname WHERE `+appSchemas)
	seqs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (struct {
		name string
		v    *int64
	}, error) {
		var x struct {
			name string
			v    *int64
		}
		err := r.Scan(&x.name, &x.v)
		return x, err
	})
	if err != nil {
		return m, err
	}
	for _, s := range seqs {
		m.Sequences[s.name] = s.v
	}

	bw := bufio.NewWriterSize(w, 1<<16)
	head, _ := json.Marshal(m)
	fmt.Fprintf(bw, "%s\n%s\n", magic, head)
	counts := map[string]int64{}
	var total int64
	for i, t := range m.Tables {
		fmt.Fprintf(bw, "TABLE %s\n", t.Name)
		tag, err := tx.Conn().PgConn().CopyTo(ctx, bw, fmt.Sprintf("COPY (SELECT %s FROM %s) TO STDOUT", quoteCols(t.Columns), ident(t.Name)))
		if err != nil {
			return m, fmt.Errorf("%s: %w", t.Name, err)
		}
		bw.WriteString("\\.\n")
		counts[t.Name] = tag.RowsAffected()
		total += tag.RowsAffected()
		if progress != nil {
			progress(i+1, len(m.Tables), total)
		}
	}
	tail, _ := json.Marshal(trailer{Rows: counts})
	fmt.Fprintf(bw, "END %s\n", tail)
	return m, bw.Flush()
}

// ---------- encryption ----------

// scryptWorkFactor 2^17: about 128 MB and half a second to open, which
// makes guessing a passphrase slow without straining a small server.
const scryptWorkFactor = 17

type sealer struct {
	gz *gzip.Writer
	aw io.WriteCloser
}

func (s sealer) Write(p []byte) (int, error) { return s.gz.Write(p) }
func (s sealer) Close() error {
	if err := s.gz.Close(); err != nil {
		return err
	}
	return s.aw.Close()
}

// seal returns a writer that compresses and encrypts into w; nothing is
// written to w unencrypted (PL-NFR-04). Close finishes the file.
func seal(w io.Writer, passphrase string) (io.WriteCloser, error) {
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, err
	}
	r.SetWorkFactor(scryptWorkFactor)
	aw, err := age.Encrypt(w, r)
	if err != nil {
		return nil, err
	}
	gz, _ := gzip.NewWriterLevel(aw, gzip.BestSpeed) // low CPU while classes run
	return sealer{gz, aw}, nil
}

// ErrPassphrase means the passphrase doesn't open the backup.
var ErrPassphrase = errors.New("the passphrase doesn't open this backup, or the file isn't a backup")

// Open decrypts and decompresses a backup file.
func Open(r io.Reader, passphrase string) (io.Reader, error) {
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, err
	}
	ar, err := age.Decrypt(r, id)
	if err != nil {
		return nil, ErrPassphrase
	}
	gz, err := gzip.NewReader(ar)
	if err != nil {
		return nil, fmt.Errorf("the backup is damaged: %w", err)
	}
	return gz, nil
}

// ---------- reading ----------

type parser struct {
	br *bufio.Reader
	m  Manifest
}

func newParser(r io.Reader) (*parser, error) {
	p := &parser{br: bufio.NewReaderSize(r, 1<<16)}
	l, err := p.br.ReadBytes('\n')
	if err != nil || string(bytes.TrimSpace(l)) != magic {
		return nil, errors.New("not a Classroom Quiz backup")
	}
	if l, err = p.br.ReadBytes('\n'); err != nil {
		return nil, fmt.Errorf("the backup is damaged: %w", err)
	}
	if err := json.Unmarshal(l, &p.m); err != nil {
		return nil, fmt.Errorf("the backup is damaged: %w", err)
	}
	return p, nil
}

// next returns the next table's name and a reader of its rows, or the trailer.
func (p *parser) next() (string, *section, *trailer, error) {
	l, err := p.br.ReadBytes('\n')
	if err != nil {
		return "", nil, nil, fmt.Errorf("the backup ends early: %w", err)
	}
	switch {
	case bytes.HasPrefix(l, []byte("TABLE ")):
		return string(bytes.TrimSpace(l[6:])), &section{br: p.br}, nil, nil
	case bytes.HasPrefix(l, []byte("END ")):
		var t trailer
		if err := json.Unmarshal(l[4:], &t); err != nil {
			return "", nil, nil, fmt.Errorf("the backup is damaged: %w", err)
		}
		return "", nil, &t, nil
	}
	return "", nil, nil, errors.New("the backup is damaged: unexpected line")
}

// section reads one table's COPY rows up to the "\." line. COPY text escapes
// backslashes, so no row can be that line.
type section struct {
	br   *bufio.Reader
	buf  []byte
	done bool
	rows int64
}

func (s *section) Read(p []byte) (int, error) {
	for len(s.buf) == 0 {
		if s.done {
			return 0, io.EOF
		}
		l, err := s.br.ReadBytes('\n')
		if err != nil {
			return 0, fmt.Errorf("the backup ends early: %w", err)
		}
		if string(l) == "\\.\n" {
			s.done = true
			return 0, io.EOF
		}
		s.rows++
		s.buf = l
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

// Check reads a whole decrypted backup and checks every table's row count
// against its trailer, without a database (`server check-backup`).
func Check(r io.Reader) (Manifest, map[string]int64, error) {
	p, err := newParser(r)
	if err != nil {
		return Manifest{}, nil, err
	}
	counts := map[string]int64{}
	for {
		name, sec, end, err := p.next()
		if err != nil {
			return p.m, counts, err
		}
		if end != nil {
			return p.m, counts, compareCounts(p.m, counts, end.Rows)
		}
		if _, err := io.Copy(io.Discard, sec); err != nil {
			return p.m, counts, err
		}
		counts[name] = sec.rows
	}
}

func compareCounts(m Manifest, got, want map[string]int64) error {
	for _, t := range m.Tables {
		if got[t.Name] != want[t.Name] {
			return fmt.Errorf("the backup is damaged: %s has %d rows, expected %d", t.Name, got[t.Name], want[t.Name])
		}
	}
	return nil
}

// ---------- restoring ----------

// Restore loads a decrypted backup into an empty database: it builds the
// schema the backup was made with, loads every table, checks the row
// counts, and restores sequences, all in one transaction. The server's
// normal start then applies any newer migrations.
func Restore(ctx context.Context, pool *pgxpool.Pool, r io.Reader, fsys embed.FS) (Manifest, map[string]int64, error) {
	var tables int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'r' AND (`+appSchemas+` OR (n.nspname = 'public' AND c.relname = 'schema_migrations'))`).Scan(&tables); err != nil {
		return Manifest{}, nil, err
	}
	if tables > 0 {
		return Manifest{}, nil, errors.New("the database isn't empty; restore into a new, empty database")
	}
	p, err := newParser(r)
	if err != nil {
		return Manifest{}, nil, err
	}
	m := p.m
	known, err := db.Versions(fsys)
	if err != nil {
		return m, nil, err
	}
	if len(m.Migrations) == 0 || len(m.Migrations) > len(known) || !slices.Equal(known[:len(m.Migrations)], m.Migrations) {
		return m, nil, errors.New("this backup was made by a newer or different version of the platform; restore it with that version")
	}
	if err := db.MigrateTo(ctx, pool, fsys, m.Migrations[len(m.Migrations)-1]); err != nil {
		return m, nil, err
	}
	specs := map[string]TableSpec{}
	for _, t := range m.Tables {
		specs[t.Name] = t
	}
	counts := map[string]int64{}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		// Foreign keys come off while loading, since rows arrive table by
		// table, and go back on afterwards, which checks every reference.
		rows, _ := tx.Query(ctx, `SELECT c.conrelid::regclass::text, c.conname, pg_get_constraintdef(c.oid)
			FROM pg_constraint c JOIN pg_namespace n ON n.oid = c.connamespace WHERE c.contype = 'f' AND `+appSchemas)
		type fk struct{ table, name, def string }
		fks, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (fk, error) {
			var f fk
			err := r.Scan(&f.table, &f.name, &f.def)
			return f, err
		})
		if err != nil {
			return err
		}
		for _, f := range fks {
			if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", f.table, pgx.Identifier{f.name}.Sanitize())); err != nil {
				return err
			}
		}
		// Migrations may have seeded rows (default policy, limits); the backup's win.
		names := make([]string, 0, len(m.Tables))
		for _, t := range m.Tables {
			names = append(names, ident(t.Name))
		}
		if len(names) > 0 {
			if _, err := tx.Exec(ctx, "TRUNCATE "+strings.Join(names, ", ")); err != nil {
				return err
			}
		}
		for {
			name, sec, end, err := p.next()
			if err != nil {
				return err
			}
			if end != nil {
				if err := compareCounts(m, counts, end.Rows); err != nil {
					return err
				}
				break
			}
			spec, ok := specs[name]
			if !ok {
				return fmt.Errorf("the backup is damaged: unknown table %s", name)
			}
			if _, err := tx.Conn().PgConn().CopyFrom(ctx, sec, fmt.Sprintf("COPY %s (%s) FROM STDIN", ident(name), quoteCols(spec.Columns))); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			counts[name] = sec.rows
		}
		for _, f := range fks {
			if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s", f.table, pgx.Identifier{f.name}.Sanitize(), f.def)); err != nil {
				return fmt.Errorf("checking references: %w", err)
			}
		}
		for seq, v := range m.Sequences {
			if v == nil {
				continue
			}
			if _, err := tx.Exec(ctx, `SELECT setval($1::regclass, $2, true)`, ident(seq), *v); err != nil {
				return err
			}
		}
		return nil
	})
	return m, counts, err
}
