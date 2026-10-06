package utils

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func Test_GetNamespacedNameForClusterInfo(t *testing.T) {
	type args struct {
		managedCluster clusterv1.ManagedCluster
	}
	tests := []struct {
		name    string
		args    args
		want    types.NamespacedName
		wantErr bool
	}{
		{
			name: "Valid Namespaced Name Claim",
			args: args{
				managedCluster: clusterv1.ManagedCluster{
					Status: clusterv1.ManagedClusterStatus{
						ClusterClaims: []clusterv1.ManagedClusterClaim{
							{
								Name:  OdfInfoClusterClaimNamespacedName,
								Value: "namespace/name",
							},
						},
					},
				},
			},
			want:    types.NamespacedName{Namespace: "namespace", Name: "name"},
			wantErr: false,
		},
		{
			name: "Missing Namespaced Name Claim",
			args: args{
				managedCluster: clusterv1.ManagedCluster{
					Status: clusterv1.ManagedClusterStatus{
						ClusterClaims: []clusterv1.ManagedClusterClaim{},
					},
				},
			},
			want:    types.NamespacedName{},
			wantErr: true,
		},
		{
			name: "Invalid Format for Namespaced Name Claim",
			args: args{
				managedCluster: clusterv1.ManagedCluster{
					Status: clusterv1.ManagedClusterStatus{
						ClusterClaims: []clusterv1.ManagedClusterClaim{
							{
								Name:  OdfInfoClusterClaimNamespacedName,
								Value: "invalidformat",
							},
						},
					},
				},
			},
			want:    types.NamespacedName{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetNamespacedNameForClusterInfo(tt.args.managedCluster)
			if (err != nil) != tt.wantErr {
				t.Errorf("getNamespacedNameForClusterInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getNamespacedNameForClusterInfo() = %v, want %v", got, tt.want)
			}
		})
	}
}
