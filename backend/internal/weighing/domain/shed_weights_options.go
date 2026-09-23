package domain

import "context"

type shedWeightsOptionsKey struct{}

type ShedWeightsOptions struct {
	IncludeLoads bool
	IncludeDates bool
}

func WithShedWeightsOptions(ctx context.Context, opts ShedWeightsOptions) context.Context {
	return context.WithValue(ctx, shedWeightsOptionsKey{}, opts)
}

func ShedWeightsOptionsFromContext(ctx context.Context) ShedWeightsOptions {
	if opts, ok := ctx.Value(shedWeightsOptionsKey{}).(ShedWeightsOptions); ok {
		return opts
	}
	return ShedWeightsOptions{IncludeLoads: true, IncludeDates: true}
}
