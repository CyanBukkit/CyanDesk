package gateway

import (
	"context"
	"time"
)

func contextTimeout(d time.Duration) (context.Context, func()) {
	return context.WithTimeout(context.Background(), d)
}
