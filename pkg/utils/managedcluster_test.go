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
			got, err := GetNamespacedNameFromClusterClaim(&tt.args.managedCluster, OdfInfoClusterClaimNamespacedName)
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

func Test_GetAllStorageVendorConfigMaps(t *testing.T) {
	tests := []struct {
		name           string
		managedCluster clusterv1.ManagedCluster
		want           map[string]types.NamespacedName
	}{
		{
			name: "ODF only",
			managedCluster: clusterv1.ManagedCluster{
				Status: clusterv1.ManagedClusterStatus{
					ClusterClaims: []clusterv1.ManagedClusterClaim{
						{
							Name:  OdfInfoClusterClaimNamespacedName,
							Value: "openshift-storage/odf-info",
						},
					},
				},
			},
			want: map[string]types.NamespacedName{
				OdfInfoClusterClaimNamespacedName: {Namespace: "openshift-storage", Name: "odf-info"},
			},
		},
		{
			name: "IBM Storage Scale only",
			managedCluster: clusterv1.ManagedCluster{
				Status: clusterv1.ManagedClusterStatus{
					ClusterClaims: []clusterv1.ManagedClusterClaim{
						{
							Name:  IbmScaleClusterClaimNamespacedName,
							Value: "ibm-spectrum-scale/ibm-spectrum-scale-replicationinfo",
						},
					},
				},
			},
			want: map[string]types.NamespacedName{
				IbmScaleClusterClaimNamespacedName: {Namespace: "ibm-spectrum-scale", Name: "ibm-spectrum-scale-replicationinfo"},
			},
		},
		{
			name: "Both ODF and IBM Storage Scale",
			managedCluster: clusterv1.ManagedCluster{
				Status: clusterv1.ManagedClusterStatus{
					ClusterClaims: []clusterv1.ManagedClusterClaim{
						{
							Name:  OdfInfoClusterClaimNamespacedName,
							Value: "openshift-storage/odf-info",
						},
						{
							Name:  IbmScaleClusterClaimNamespacedName,
							Value: "ibm-spectrum-scale/ibm-spectrum-scale-replicationinfo",
						},
					},
				},
			},
			want: map[string]types.NamespacedName{
				OdfInfoClusterClaimNamespacedName:  {Namespace: "openshift-storage", Name: "odf-info"},
				IbmScaleClusterClaimNamespacedName: {Namespace: "ibm-spectrum-scale", Name: "ibm-spectrum-scale-replicationinfo"},
			},
		},
		{
			name: "No storage vendor claims",
			managedCluster: clusterv1.ManagedCluster{
				Status: clusterv1.ManagedClusterStatus{
					ClusterClaims: []clusterv1.ManagedClusterClaim{},
				},
			},
			want: map[string]types.NamespacedName{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetAllStorageVendorConfigMaps(&tt.managedCluster)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetAllStorageVendorConfigMaps() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_HasRequiredStorageVendorKey(t *testing.T) {
	tests := []struct {
		name           string
		managedCluster clusterv1.ManagedCluster
		want           bool
	}{
		{
			name: "Has ODF claim",
			managedCluster: clusterv1.ManagedCluster{
				Status: clusterv1.ManagedClusterStatus{
					ClusterClaims: []clusterv1.ManagedClusterClaim{
						{Name: OdfInfoClusterClaimNamespacedName, Value: "ns/name"},
					},
				},
			},
			want: true,
		},
		{
			name: "Has IBM Storage Scale claim",
			managedCluster: clusterv1.ManagedCluster{
				Status: clusterv1.ManagedClusterStatus{
					ClusterClaims: []clusterv1.ManagedClusterClaim{
						{Name: IbmScaleClusterClaimNamespacedName, Value: "ns/name"},
					},
				},
			},
			want: true,
		},
		{
			name: "Has both ODF and IBM Storage Scale claims",
			managedCluster: clusterv1.ManagedCluster{
				Status: clusterv1.ManagedClusterStatus{
					ClusterClaims: []clusterv1.ManagedClusterClaim{
						{Name: OdfInfoClusterClaimNamespacedName, Value: "ns/name"},
						{Name: IbmScaleClusterClaimNamespacedName, Value: "ns/name"},
					},
				},
			},
			want: true,
		},
		{
			name: "No storage vendor claims",
			managedCluster: clusterv1.ManagedCluster{
				Status: clusterv1.ManagedClusterStatus{
					ClusterClaims: []clusterv1.ManagedClusterClaim{
						{Name: "some-other-claim", Value: "value"},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasRequiredStorageVendorKey(&tt.managedCluster); got != tt.want {
				t.Errorf("HasRequiredStorageVendorKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_HasRequiredIbmScaleKey(t *testing.T) {
	tests := []struct {
		name           string
		managedCluster clusterv1.ManagedCluster
		want           bool
	}{
		{
			name: "Has IBM Storage Scale claim",
			managedCluster: clusterv1.ManagedCluster{
				Status: clusterv1.ManagedClusterStatus{
					ClusterClaims: []clusterv1.ManagedClusterClaim{
						{Name: IbmScaleClusterClaimNamespacedName, Value: "ns/name"},
					},
				},
			},
			want: true,
		},
		{
			name: "Does not have IBM Storage Scale claim",
			managedCluster: clusterv1.ManagedCluster{
				Status: clusterv1.ManagedClusterStatus{
					ClusterClaims: []clusterv1.ManagedClusterClaim{
						{Name: OdfInfoClusterClaimNamespacedName, Value: "ns/name"},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasRequiredIbmScaleKey(&tt.managedCluster); got != tt.want {
				t.Errorf("HasRequiredIbmScaleKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
