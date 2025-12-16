package main

import (
	"flag"
	"reflect"
	"testing"
)

func Test_gatherOptions(t *testing.T) {
	cases := []struct {
		name     string
		args     map[string]string
		expected func(*options)
		err      bool
	}{
		{
			name: "minimal flags work",
		},
		{
			name: "explicitly set --gitee-token",
			args: map[string]string{
				"--gitee-token": "/random/value",
			},
			expected: func(o *options) {
				o.giteeToken = "/random/value"
			},
		},
		{
			name: "explicitly set --webhook-secret",
			args: map[string]string{
				"--webhook-secret": "/random/value",
			},
			expected: func(o *options) {
				o.webhookSecret = "/random/value"
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expected := &options{
				port:          8765,
				giteeToken:    "token.conf",
				webhookSecret: "secret.conf",
			}
			if tc.expected != nil {
				tc.expected(expected)
			}

			argMap := map[string]string{}
			for k, v := range tc.args {
				argMap[k] = v
			}
			var args []string
			for k, v := range argMap {
				args = append(args, k+"="+v)
			}
			fs := flag.NewFlagSet("fake-flags", flag.PanicOnError)
			actual := gatherOptions(fs, args...)
			switch err := actual.Validate(); {
			case err != nil:
				if !tc.err {
					t.Errorf("unexpected error: %v", err)
				}
			case tc.err:
				t.Errorf("failed to receive expected error")
			case !reflect.DeepEqual(*expected, actual):
				t.Errorf("%#v != expected %#v", actual, *expected)
			}
		})
	}
}
