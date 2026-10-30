package task

import (
	"context"
)

var (
	Ctx, Cancel = context.WithCancel(context.Background())
)
