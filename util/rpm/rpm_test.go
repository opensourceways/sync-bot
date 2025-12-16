package rpm

import (
	"testing"
)

func TestSpec(t *testing.T) {
	type args struct {
		data string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "no Version, no Release",
			args: args{
				data: ``,
			},
			want: []string{"", ""},
		},
		{
			name: "Values from literal",
			args: args{
				data: `
Name:          unit-api
Version:       1.0
Release:       6
Summary:       JSR 363 - Units of Measurement API
License:       BSD
`,
			},
			want: []string{"1.0", "6"},
		},
		{
			name: "Values from marcos",
			args: args{
				data: `
%global upstream_version    5.10
%global upstream_sublevel   0
%global devel_release       4
%global maintenance_release .0.0
%global pkg_release         .13

Name:    kernel
Version: %{upstream_version}.%{upstream_sublevel}
Release: %{devel_release}%{?maintenance_release}%{?pkg_release}%{?extra_release}
`,
			},
			want: []string{"5.10.0", "4.0.0.13%{?extra_release}"},
		},
		{
			name: "Nested macros",
			args: args{
				data: `
%global baserelease 4
%global nodejs_epoch 1
%global nodejs_major 12
%global nodejs_minor 18
%global nodejs_patch 4
%global nodejs_version %{nodejs_major}.%{nodejs_minor}.%{nodejs_patch}
%global nodejs_release %{baserelease}

Version: %{nodejs_version}
Release: %{nodejs_release}
`,
			},
			want: []string{"12.18.4", "4"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSpec(tt.args.data)
			if got := s.Version(); got != tt.want[0] {
				t.Errorf("Version() = %v, want %v", got, tt.want[0])
			}
			if got := s.Release(); got != tt.want[1] {
				t.Errorf("Release() = %v, want %v", got, tt.want[1])
			}
		})
	}
}
