package adminui

import (
	"context"
	"sync"

	"github.com/anlekg/quarel/internal/settings"
)

// Supervisor runs the service, restarts it on request (new settings) and
// keeps the admin interface informed. A service that cannot start waits for
// new settings instead of ending the program.
type Supervisor struct {
	restart chan struct{}
	resume  chan struct{}
	mu      sync.Mutex
	paused  bool
	stopped chan struct{} // closed when the current run has ended
}

// NewSupervisor returns an idle supervisor.
func NewSupervisor() *Supervisor {
	s := &Supervisor{restart: make(chan struct{}, 1), resume: make(chan struct{}, 1), stopped: make(chan struct{})}
	close(s.stopped)
	return s
}

// Restart asks for the service to be restarted (returns immediately).
func (s *Supervisor) Restart() {
	select {
	case s.restart <- struct{}{}:
	default:
	}
}

// Pause stops the service and waits until it has stopped (for a restore);
// the returned function starts it again.
func (s *Supervisor) Pause() func() {
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
	s.Restart()
	for {
		s.mu.Lock()
		done := s.stopped
		s.mu.Unlock()
		<-done
		// The loop may have been between two runs: make sure no new run started.
		s.mu.Lock()
		same := done == s.stopped
		s.mu.Unlock()
		if same {
			break
		}
	}
	return func() {
		s.mu.Lock()
		s.paused = false
		s.mu.Unlock()
		select {
		case s.resume <- struct{}{}:
		default:
		}
	}
}

func (s *Supervisor) isPaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

// Run loads the settings and runs serve until ctx ends, restarting it when
// asked. serve must return once its context is cancelled, after releasing
// everything (ports, processes, database); it calls ready once serving.
func (s *Supervisor) Run(ctx context.Context, ui *UI, serve func(ctx context.Context, ready func()) error) {
	for ctx.Err() == nil {
		for s.isPaused() {
			ui.SetState(Stopped, nil)
			select {
			case <-s.resume:
			case <-ctx.Done():
				return
			}
		}
		// Drop a restart request made while stopped: this run uses the latest settings.
		select {
		case <-s.restart:
		default:
		}
		var err error
		if err = settings.Load(settings.DataDir()); err == nil {
			err = s.runOnce(ctx, ui, serve)
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			ui.SetState(Failed, err)
			select { // wait for new settings (or a restart request)
			case <-s.restart:
			case <-s.resume:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (s *Supervisor) runOnce(ctx context.Context, ui *UI, serve func(context.Context, func()) error) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	s.mu.Lock()
	s.stopped = done
	s.mu.Unlock()
	defer close(done)

	ui.SetState(Starting, nil)
	errc := make(chan error, 1)
	go func() { errc <- serve(runCtx, func() { ui.SetState(Running, nil) }) }()
	select {
	case err := <-errc:
		return err // stopped by itself: a failure
	case <-s.restart:
		cancel()
		<-errc
		return nil
	case <-ctx.Done():
		cancel()
		<-errc
		return nil
	}
}
