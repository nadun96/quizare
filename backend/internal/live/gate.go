package live

import "context"

// Gate refuses new activities while the admin has paused them, for
// everyone or for this teacher (D-90).
type Gate func(ctx context.Context, teacherID string) error

// Check passes when there is no gate.
func (g Gate) Check(ctx context.Context, teacherID string) error {
	if g == nil {
		return nil
	}
	return g(ctx, teacherID)
}

// SetGate installs the check made before an activity starts.
func (s *Service) SetGate(g Gate) { s.gate = g }

// ForceEnd ends a session for the admin, whoever runs it (D-90), as End
// does for its teacher.
func (s *Service) ForceEnd(ctx context.Context, actorID, sessionID string) error {
	sess, err := s.session(ctx, sessionID)
	if err != nil {
		return err
	}
	return s.endSession(ctx, sess, actorID)
}
