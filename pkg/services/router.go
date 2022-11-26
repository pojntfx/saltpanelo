package services

import "context"

type Router struct{}

func (r *Router) CalculatePath(ctx context.Context, source string, destination string) ([]string, error) {
	return []string{}, nil
}
