package main

import "os"

func resetStore(s *Store) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blocks = map[string]*Block{}
	s.order = nil
	s.versions = map[int64]*Snapshot{}
	s.current = 0
	err := os.Remove(s.logPath())
	if os.IsNotExist(err) {
		err = nil
	}
	return err
}
