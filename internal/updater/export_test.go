package updater

// SetRenameFuncForTest overrides the rename operation during apply for unit tests.
func SetRenameFuncForTest(s *Service, fn func(oldpath, newpath string) error) {
	s.mu.Lock()
	s.renameFn = fn
	s.mu.Unlock()
}
