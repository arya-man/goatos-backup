package main

import (
	"testing"

	"cloud.google.com/go/bigquery"
)

func TestNullInt64OrZero(t *testing.T) {
	cases := []struct {
		name string
		in   bigquery.NullInt64
		want int64
	}{
		{"valid positive value passes through", bigquery.NullInt64{Int64: 42, Valid: true}, 42},
		{"valid zero value passes through", bigquery.NullInt64{Int64: 0, Valid: true}, 0},
		{"invalid (NULL) becomes zero", bigquery.NullInt64{Int64: 999, Valid: false}, 0},
		{"zero-value struct (never set) becomes zero", bigquery.NullInt64{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nullInt64OrZero(tc.in); got != tc.want {
				t.Fatalf("nullInt64OrZero(%+v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
