package storage

// SetDisk replaces the free-space check, so tests can run out of disk.
func (s *Service) SetDisk(f func(path string) (free, total uint64, err error)) { s.disk = f }
