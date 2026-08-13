package ui

import "github.com/lxn/walk"

const applicationIconResourceID = 1

func loadApplicationIcon() (*walk.Icon, error) {
	return walk.NewIconFromResourceId(applicationIconResourceID)
}
