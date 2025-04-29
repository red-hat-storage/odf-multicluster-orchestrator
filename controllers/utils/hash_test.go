package utils

import (
	"testing"
)

func TestFnvHashTest(t *testing.T) {
	testCases := []struct {
		input          string
		expectedOutput uint32
	}{
		{"5m", 1625360775},
		{"10m", 473128587},
		{"1h", 1676385180},
	}

	for _, testCase := range testCases {
		actualOutput := FnvHash(testCase.input)
		if actualOutput != testCase.expectedOutput {
			t.Errorf("FnvHash(%s) = %d, expected %d", testCase.input, actualOutput, testCase.expectedOutput)
		}
	}
}

func TestCreateUniqueReplicationId(t *testing.T) {
	type args struct {
		storageIds map[string]string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr string // Changed to string to check error message
	}{
		{
			name: "Case 1: Both FSID available",
			args: args{
				storageIds: map[string]string{
					"c1": "7e252ee3-abd9-4c54-a4ff-a2fdce8931a0",
					"c2": "aacfbd7e-5ced-42a5-bdc2-483fcbe5a29d",
				},
			},
			want:    "a52e5dd4473c6fedbcf084d27a84c9bb",
			wantErr: "",
		},
		{
			name: "Case 2: FSID for C1 is available, C2 empty",
			args: args{
				storageIds: map[string]string{
					"c1": "7e252ee3-abd9-4c54-a4ff-a2fdce8931a0",
					"c2": "",
				},
			},
			want:    "",
			wantErr: "replicationID can not be generated due to missing cluster StorageIds",
		},
		{
			name: "Case 3: FSID for C2 is available, C1 empty",
			args: args{
				storageIds: map[string]string{
					"c1": "",
					"c2": "aacfbd7e-5ced-42a5-bdc2-483fcbe5a29d",
				},
			},
			want:    "",
			wantErr: "replicationID can not be generated due to missing cluster StorageIds",
		},
		{
			name: "Case 4: Both FSID unavailable",
			args: args{
				storageIds: map[string]string{
					"c1": "",
					"c2": "",
				},
			},
			want:    "",
			wantErr: "replicationID can not be generated due to missing cluster StorageIds",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ids []string
			for _, id := range []string{tt.args.storageIds["c1"], tt.args.storageIds["c2"]} {
				if id != "" {
					ids = append(ids, id)
				}
			}

			got, err := CreateUniqueReplicationId(ids...)

			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("CreateUniqueReplicationId() expected error = %v, got no error", tt.wantErr)
					return
				}
				if err.Error() != tt.wantErr {
					t.Errorf("CreateUniqueReplicationId() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				return
			}
			if err != nil {
				t.Errorf("CreateUniqueReplicationId() unexpected error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("CreateUniqueReplicationId() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateMD5Hash(t *testing.T) {
	type args struct {
		value any
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "RBD with empty CephFSID",
			args: args{
				value: [2]string{"", ""},
			},
			want: "2432510f5993984b053e7d74ce53d94c",
		},
		{
			name: "CephFS with empty CephFSID",
			args: args{
				value: [2]string{"", "csi"},
			},
			want: "793b48b9b17bdad5cd141e6daadfdd4e",
		},
		{
			name: "C1/RBD",
			args: args{
				value: [2]string{"a0eaa723-bf87-48db-ad11-17e276edda61", ""},
			},
			want: "bb2361459436fd47121419c658d4c79b",
		},
		{
			name: "C1/CephFS",
			args: args{
				value: [2]string{"a0eaa723-bf87-48db-ad11-17e276edda61", "csi"},
			},
			want: "41711cb35464e85f4446ace583adbaa1",
		},
		{
			name: "C2/RBD",
			args: args{
				value: [2]string{"30dc85db-f35f-4c03-bb43-c8592487d8b9", ""},
			},
			want: "9e4fbb3d9e52875d6393409a51927243",
		},
		{
			name: "C2/CephFS",
			args: args{
				value: [2]string{"30dc85db-f35f-4c03-bb43-c8592487d8b9", "csi"},
			},
			want: "4b7e331b508946ca7165f14804c37ffc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CalculateMD5Hash(tt.args.value); got != tt.want {
				t.Errorf("CalculateMD5Hash() = %v, want %v", got, tt.want)
			}
		})
	}
}
