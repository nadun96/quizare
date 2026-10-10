// Package media talks to the media server, LiveKit (ADR-19, D-58): it signs
// the short-lived access tokens browsers join with (TS-NFR-21) and calls
// LiveKit's server API to change permissions live, mute tracks and end
// rooms (TS-FR-21, TS-FR-23, TS-FR-04). The API is Twirp; its JSON form is
// used, so no LiveKit SDK is needed.
package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Sources a participant may publish. These are LiveKit's names in tokens.
const (
	Camera      = "camera"
	Microphone  = "microphone"
	Screen      = "screen_share"
	ScreenAudio = "screen_share_audio"
)

// Grant is what one person may do in one room.
type Grant struct {
	Room      string
	Identity  string
	Name      string
	Metadata  string
	Subscribe bool
	Sources   []string // nothing = may not publish (TS-FR-20)
}

type videoGrant struct {
	Room              string   `json:"room,omitempty"`
	RoomJoin          bool     `json:"roomJoin,omitempty"`
	RoomAdmin         bool     `json:"roomAdmin,omitempty"`
	RoomCreate        bool     `json:"roomCreate,omitempty"`
	RoomList          bool     `json:"roomList,omitempty"`
	CanPublish        *bool    `json:"canPublish,omitempty"`
	CanSubscribe      *bool    `json:"canSubscribe,omitempty"`
	CanPublishData    *bool    `json:"canPublishData,omitempty"`
	CanPublishSources []string `json:"canPublishSources,omitempty"`
}

type claims struct {
	jwt.RegisteredClaims
	Name     string     `json:"name,omitempty"`
	Metadata string     `json:"metadata,omitempty"`
	Video    videoGrant `json:"video"`
}

// TokenTTL: a copied token is useless after 10 minutes (TS-NFR-21); the
// browser fetches a new one to reconnect.
const TokenTTL = 10 * time.Minute

type Client struct {
	url       string // the server API, on the server's network
	apiKey    string
	apiSecret []byte
	http      *http.Client
	now       func() time.Time
}

func New(url, apiKey string, apiSecret []byte) *Client {
	return &Client{url: strings.TrimRight(url, "/"), apiKey: apiKey, apiSecret: apiSecret, http: &http.Client{Timeout: 5 * time.Second}, now: time.Now}
}

func ptr(b bool) *bool { return &b }

func (c *Client) sign(subject, name, metadata string, v videoGrant, ttl time.Duration) (string, error) {
	now := c.now()
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		RegisteredClaims: jwt.RegisteredClaims{Issuer: c.apiKey, Subject: subject, NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)), ExpiresAt: jwt.NewNumericDate(now.Add(ttl))},
		Name:             name, Metadata: metadata, Video: v,
	}).SignedString(c.apiSecret)
}

// Token signs a browser's access token. Data messages are off: chat and
// hands go through the tutoring service, which stores and checks them.
func (c *Client) Token(g Grant) (string, error) {
	sources := g.Sources
	if sources == nil {
		sources = []string{}
	}
	return c.sign(g.Identity, g.Name, g.Metadata, videoGrant{Room: g.Room, RoomJoin: true, CanSubscribe: ptr(g.Subscribe),
		CanPublish: ptr(len(sources) > 0), CanPublishData: ptr(false), CanPublishSources: sources}, TokenTTL)
}

// call makes one server-API request with a short admin token for room.
func (c *Client) call(ctx context.Context, method, room string, in, out any) error {
	tok, err := c.sign("tutoring-service", "", "", videoGrant{Room: room, RoomAdmin: true, RoomCreate: true, RoomList: true}, time.Minute)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/twirp/livekit.RoomService/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("media server: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotInRoom
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("media server %s: %d %s", method, resp.StatusCode, raw)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// ErrNotInRoom: the person (or room) isn't on the media server right now;
// for permission changes that's fine, their next token carries the change.
var ErrNotInRoom = fmt.Errorf("not connected to the media server")

// Twirp's JSON uses the protobuf enum names for track sources.
var sourceEnum = map[string]string{Camera: "CAMERA", Microphone: "MICROPHONE", Screen: "SCREEN_SHARE", ScreenAudio: "SCREEN_SHARE_AUDIO"}

// SetPermission changes what a connected person may publish, at once
// (TS-FR-21). LiveKit unpublishes tracks whose source is no longer allowed.
func (c *Client) SetPermission(ctx context.Context, room, identity string, subscribe bool, sources []string) error {
	enums := []string{}
	for _, s := range sources {
		enums = append(enums, sourceEnum[s])
	}
	return c.call(ctx, "UpdateParticipant", room, map[string]any{"room": room, "identity": identity,
		"permission": map[string]any{"canSubscribe": subscribe, "canPublish": len(sources) > 0, "canPublishData": false, "canPublishSources": enums}}, nil)
}

// Track is one published track.
type Track struct {
	Sid    string `json:"sid"`
	Source string `json:"source"` // CAMERA, MICROPHONE, SCREEN_SHARE…
	Muted  bool   `json:"muted"`
}

// Participant is one person connected to a room.
type Participant struct {
	Identity string  `json:"identity"`
	Tracks   []Track `json:"tracks"`
}

func (c *Client) Participants(ctx context.Context, room string) ([]Participant, error) {
	var out struct {
		Participants []Participant `json:"participants"`
	}
	err := c.call(ctx, "ListParticipants", room, map[string]any{"room": room}, &out)
	return out.Participants, err
}

// Mute mutes a person's published tracks of the given sources (TS-FR-23).
func (c *Client) Mute(ctx context.Context, room, identity string, sources ...string) error {
	ps, err := c.Participants(ctx, room)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, s := range sources {
		want[sourceEnum[s]] = true
	}
	for _, p := range ps {
		if p.Identity != identity {
			continue
		}
		for _, t := range p.Tracks {
			if want[t.Source] && !t.Muted {
				if err := c.call(ctx, "MutePublishedTrack", room, map[string]any{"room": room, "identity": identity, "trackSid": t.Sid, "muted": true}, nil); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Remove disconnects one person (TS-FR-60).
func (c *Client) Remove(ctx context.Context, room, identity string) error {
	return c.call(ctx, "RemoveParticipant", room, map[string]any{"room": room, "identity": identity}, nil)
}

// EndRoom disconnects everyone and stops all media (TS-FR-04).
func (c *Client) EndRoom(ctx context.Context, room string) error {
	return c.call(ctx, "DeleteRoom", room, map[string]any{"room": room}, nil)
}
