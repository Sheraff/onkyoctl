package main

import (
	"testing"

	"onkyoctl/service/internal/socketapi"
)

func TestRequestFromArgsVolume(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want socketapi.Request
	}{
		{name: "up default", args: []string{"volume", "up"}, want: socketapi.Request{Command: "volume", Direction: "up", Steps: 1}},
		{name: "down default", args: []string{"volume", "down"}, want: socketapi.Request{Command: "volume", Direction: "down", Steps: 1}},
		{name: "up steps", args: []string{"volume", "up", "5"}, want: socketapi.Request{Command: "volume", Direction: "up", Steps: 5}},
		{name: "down steps", args: []string{"volume", "down", "20"}, want: socketapi.Request{Command: "volume", Direction: "down", Steps: 20}},
		{name: "positive delta", args: []string{"volume", "+5"}, want: socketapi.Request{Command: "volume", Direction: "up", Steps: 5}},
		{name: "negative delta", args: []string{"volume", "-5"}, want: socketapi.Request{Command: "volume", Direction: "down", Steps: 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, wantsStatus, err := requestFromArgs(tc.args)
			if err != nil {
				t.Fatalf("requestFromArgs returned error: %v", err)
			}
			if wantsStatus {
				t.Fatalf("wantsStatus = true, want false")
			}
			if got != tc.want {
				t.Fatalf("request = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestRequestFromArgsRejectsBadVolume(t *testing.T) {
	for _, args := range [][]string{
		{"volume"},
		{"volume", "sideways"},
		{"volume", "up", "0"},
		{"volume", "down", "-1"},
		{"volume", "+0"},
		{"volume", "+bad"},
		{"volume", "+5", "extra"},
	} {
		if _, _, err := requestFromArgs(args); err == nil {
			t.Fatalf("requestFromArgs(%#v) succeeded, want error", args)
		}
	}
}
