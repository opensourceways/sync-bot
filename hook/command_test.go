package hook

import (
	"reflect"
	"testing"
)

func Test_parse(t *testing.T) {
	type args struct {
		cmd string
	}
	tests := []struct {
		name    string
		args    args
		want    *SyncCmdOption
		wantErr bool
	}{
		{
			name: "no branch",
			args: args{
				"/sync",
			},
			want: &SyncCmdOption{
				strategy: Pick,
				branches: []string{},
			},
			wantErr: false,
		},
		{
			name: "one branch",
			args: args{
				"/sync branch1",
			},
			want: &SyncCmdOption{
				strategy: Pick,
				branches: []string{"branch1"},
			},
			wantErr: false,
		},
		{
			name: "two branches",
			args: args{
				"/sync branch1 branch2",
			},
			want: &SyncCmdOption{
				strategy: Pick,
				branches: []string{"branch1", "branch2"},
			},
			wantErr: false,
		},
		{
			name: "superfluous options",
			args: args{
				"/sync -a --b x.spec openEuler-20.03-LTS make_build openEuler-20.09",
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "special character branch name",
			args: args{
				"/sync foo.bar foo_bar foo-bar foo/bar",
			},
			want: &SyncCmdOption{
				strategy: Pick,
				branches: []string{"foo.bar", "foo_bar", "foo-bar", "foo/bar"},
			},
			wantErr: false,
		},
		{
			name: "prefix blank line",
			args: args{
				"\n\n/sync branch1",
			},
			want: &SyncCmdOption{
				strategy: Pick,
				branches: []string{"branch1"},
			},
			wantErr: false,
		},
		{
			name: "suffix blank line",
			args: args{
				"/sync branch1\n\n",
			},
			want: &SyncCmdOption{
				strategy: Pick,
				branches: []string{"branch1"},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSyncCommand(tt.args.cmd)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSyncCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseSyncCommand() got = %v, want %v", got, tt.want)
			}
		})
	}
}
