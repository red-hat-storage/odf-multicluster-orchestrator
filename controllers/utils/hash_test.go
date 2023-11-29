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
		fsids map[string]string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "Case 1: Both FSID available",
			args: args{
				fsids: map[string]string{
					"c1": "7e252ee3-abd9-4c54-a4ff-a2fdce8931a0",
					"c2": "aacfbd7e-5ced-42a5-bdc2-483fcbe5a29d",
				},
			},
			want:    "a99df9fc6c52c7ef44222ab38657a0b15628a14",
			wantErr: false,
		},
		{
			name: "Case 2: FSID for C1 is available",
			args: args{
				fsids: map[string]string{
					"c1": "7e252ee3-abd9-4c54-a4ff-a2fdce8931a0",
					"c2": "",
				},
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "Case 3: FSID for C2 is available",
			args: args{
				fsids: map[string]string{
					"c1": "",
					"c2": "aacfbd7e-5ced-42a5-bdc2-483fcbe5a29d",
				},
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "Case 4: Both FSID unavailable",
			args: args{
				fsids: map[string]string{
					"c1": "",
					"c2": "",
				},
			},
			want:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CreateUniqueReplicationId(tt.args.fsids)
			if ((err != nil) != tt.wantErr) || got != tt.want {
				t.Errorf("CreateUniqueReplicationId() = %v, want %v", got, tt.want)
			}
		})
	}
}
