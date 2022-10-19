package gateway

import "fmt"

var (
	ErrStoreNotExists   = fmt.Errorf("gateway store doesn't exists")
	ErrNotFound         = fmt.Errorf("not found")
	ErrAlreadyExists    = fmt.Errorf("already exists")
	ErrInvalidGatewayID = fmt.Errorf("invalid gateway ID")
)
