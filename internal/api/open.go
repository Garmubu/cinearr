package api

import "cinearr/internal/desktop"

func openFolder(path string) error { return desktop.OpenPath(path) }

func pickPath(dir bool, title string) (string, error) { return desktop.Pick(dir, title) }
