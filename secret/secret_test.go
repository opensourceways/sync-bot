package secret

import (
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"testing"
)

var files []string

func setup() {
	secret1, err := ioutil.TempFile("", "")
	if err != nil {
		log.Fatalf("failed to set up a temporary file: %v", err)
	}
	if _, err1 := secret1.WriteString("SECRET"); err1 != nil {
		log.Fatalf("failed to write a fake secret to a file: %v", err)
	}
	defer secret1.Close()
	files = append(files, secret1.Name())

	secret2, err := ioutil.TempFile("", "")
	if err != nil {
		log.Fatalf("failed to set up a temporary file: %v", err)
	}
	if _, err1 := secret2.WriteString("MYSTERY"); err1 != nil {
		log.Fatalf("failed to write a fake secret to a file: %v", err)
	}
	defer secret2.Close()
	files = append(files, secret2.Name())

	_ = LoadSecrets(files)
}

func teardown() {
	for _, f := range files {
		os.Remove(f)
	}
}

func TestMain(m *testing.M) {
	setup()
	m.Run()
	teardown()
	os.Exit(0)
}

func TestGetGenerator(t *testing.T) {
	type args struct {
		secretPath string
	}
	tests := []struct {
		name string
		args args
		want func() []byte
	}{
		{"secret1",
			args{
				files[0],
			},
			func() []byte {
				return []byte("SECRET")
			},
		},
		{"secret2",
			args{
				files[1],
			},
			func() []byte {
				return []byte("MYSTERY")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetGenerator(tt.args.secretPath); !reflect.DeepEqual(got(), tt.want()) {
				t.Errorf("GetGenerator() = %v, want %v", got(), tt.want())
			}
		})
	}
}

func TestGetSecret(t *testing.T) {
	type args struct {
		secretPath string
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{"secret1",
			args{
				files[0],
			},
			[]byte("SECRET"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetSecret(tt.args.secretPath); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetSecret() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadSecrets(t *testing.T) {
	type args struct {
		paths []string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := LoadSecrets(tt.args.paths); (err != nil) != tt.wantErr {
				t.Errorf("LoadSecrets() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_loadSingleSecret(t *testing.T) {
	type args struct {
		path string
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr bool
	}{
		{
			"secret1",
			args{
				files[0],
			},
			[]byte("SECRET"),
			false,
		},
		{
			"notfound",
			args{
				"notfound",
			},
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loadSingleSecret(tt.args.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadSingleSecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("loadSingleSecret() got = %v, want %v", got, tt.want)
			}
		})
	}
}
