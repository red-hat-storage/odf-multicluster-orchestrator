package utils

import "testing"

func TestCompareSemverMajorMinorVersions(t *testing.T) {
	type args struct {
		v1  string
		v2  string
		opr operator
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "test equals",
			args: args{
				v1:  "4.19.1",
				v2:  "4.19.1",
				opr: Eq,
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "test lt",
			args: args{
				v1:  "4.19.1",
				v2:  "4.19.2",
				opr: Lt,
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "test gt",
			args: args{
				v1:  "4.19.2",
				v2:  "4.19.1",
				opr: Gt,
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "test equals",
			args: args{
				v1:  "4.19.0",
				v2:  "4.19.1",
				opr: Eq,
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "test lt",
			args: args{
				v1:  "4.19.2",
				v2:  "4.19.1",
				opr: Lt,
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "test gt",
			args: args{
				v1:  "4.19.1",
				v2:  "4.19.2",
				opr: Gt,
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "test bad operator",
			args: args{
				v1:  "4.19.1",
				v2:  "4.19.2",
				opr: "",
			},
			want:    false,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CompareSemverMajorMinorVersions(tt.args.v1, tt.args.v2, tt.args.opr)
			if (err != nil) != tt.wantErr {
				t.Errorf("CompareSemverMajorMinorVersions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("CompareSemverMajorMinorVersions() = %v, want %v", got, tt.want)
			}
		})
	}
}
