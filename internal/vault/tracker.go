package vault

import (
	"sync"
	"time"
)

// What a session last saw of a file. Advisory: used to warn about stale
// views, never to block a patch.
type seen struct {
	hash    string
	partial bool
}

type sessionState struct {
	files map[string]seen
	used  time.Time
}

type tracker struct {
	mu       sync.Mutex
	sessions map[string]*sessionState
}

const (
	maxSessions     = 256
	maxFilesPerSess = 5000
)

func newTracker() *tracker { return &tracker{sessions: map[string]*sessionState{}} }

func (t *tracker) get(session, rel string) (seen, bool) {
	if session == "" {
		return seen{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.sessions[session]
	if s == nil {
		return seen{}, false
	}
	e, ok := s.files[rel]
	return e, ok
}

func (t *tracker) set(session, rel string, e seen) {
	if session == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.sessions[session]
	if s == nil {
		if len(t.sessions) >= maxSessions {
			var oldest string
			var ot time.Time
			for id, st := range t.sessions {
				if oldest == "" || st.used.Before(ot) {
					oldest, ot = id, st.used
				}
			}
			delete(t.sessions, oldest)
		}
		s = &sessionState{files: map[string]seen{}}
		t.sessions[session] = s
	}
	if len(s.files) >= maxFilesPerSess {
		s.files = map[string]seen{}
	}
	s.used = time.Now()
	s.files[rel] = e
}
