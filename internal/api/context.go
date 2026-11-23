package api

import "context"

type ctxKey int

const reqKey ctxKey = 1

func withReqID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, reqKey, id)
}

func reqID(r interface{ Context() context.Context }) string {
	v, _ := r.Context().Value(reqKey).(string)
	return v
}
