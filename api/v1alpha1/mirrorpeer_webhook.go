/*
Copyright 2021 Red Hat OpenShift Data Foundation.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var mirrorpeerlog = logf.Log.WithName("mirrorpeer-webhook")

const (
	WebhookCertDir  = "/apiserver.local.config/certificates"
	WebhookCertName = "apiserver.crt"
	WebhookKeyName  = "apiserver.key"
)

func (r *MirrorPeer) SetupWebhookWithManager(mgr ctrl.Manager) error {
	bldr := ctrl.NewWebhookManagedBy(mgr).
		For(r)

	srv := mgr.GetWebhookServer()
	srv.CertDir = WebhookCertDir
	srv.CertName = WebhookCertName
	srv.KeyName = WebhookKeyName

	return bldr.Complete()
}

//+kubebuilder:webhook:path=/mutate-multicluster-odf-openshift-io-v1alpha1-mirrorpeer,mutating=true,failurePolicy=fail,sideEffects=None,groups=multicluster.odf.openshift.io,resources=mirrorpeers,verbs=create;update,versions=v1alpha1,name=mmirrorpeer.kb.io,admissionReviewVersions=v1;v1beta1

var _ webhook.Defaulter = &MirrorPeer{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *MirrorPeer) Default() {}

//+kubebuilder:webhook:path=/validate-multicluster-odf-openshift-io-v1alpha1-mirrorpeer,mutating=false,failurePolicy=fail,sideEffects=None,groups=multicluster.odf.openshift.io,resources=mirrorpeers,verbs=create;update;delete,versions=v1alpha1,name=vmirrorpeer.kb.io,admissionReviewVersions=v1;v1beta1

var _ webhook.Validator = &MirrorPeer{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *MirrorPeer) ValidateCreate() error {
	mirrorpeerlog.Info("validate create", "name", r.ObjectMeta.Name)

	return validateMirrorPeer(r)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *MirrorPeer) ValidateUpdate(old runtime.Object) error {
	mirrorpeerlog.Info("validate update", "name", r.ObjectMeta.Name)
	oldMirrorPeer, ok := old.(*MirrorPeer)
	if !ok {
		return fmt.Errorf("error casting old object to MirrorPeer")
	}

	if len(r.Spec.Items) != len(oldMirrorPeer.Spec.Items) {
		return fmt.Errorf("error updating MirrorPeer, new and old spec.items have different lengths")
	}

	if r.Spec.Type != oldMirrorPeer.Spec.Type {
		return fmt.Errorf("error updating MirrorPeer, the type cannot be changed from %s to %s", oldMirrorPeer.Spec.Type, r.Spec.Type)
	}

	refs := make(map[string]int)
	for idx, pr := range oldMirrorPeer.Spec.Items {
		// Creating a make-shift set of references to check for duplicates
		refs[fmt.Sprintf("%s-%s-%s", pr.ClusterName, pr.StorageClusterRef.Namespace, pr.StorageClusterRef.Name)] = idx
	}

	for _, pr := range r.Spec.Items {
		key := fmt.Sprintf("%s-%s-%s", pr.ClusterName, pr.StorageClusterRef.Namespace, pr.StorageClusterRef.Name)
		if _, ok := refs[key]; !ok {
			return fmt.Errorf("error validating update: new MirrorPeer %s references a StorageCluster %s/%s that is not in the old MirrorPeer", r.ObjectMeta.Name, pr.StorageClusterRef.Namespace, pr.StorageClusterRef.Name)
		}
	}

	if oldMirrorPeer.Spec.OverlappingCIDR && !r.Spec.OverlappingCIDR {
		return fmt.Errorf("error updating MirrorPeer: OverlappingCIDR value can not be changed from %t to %t. This is to prevent Disaster Recovery from being unusable between clusters that have overlapping IPs", oldMirrorPeer.Spec.OverlappingCIDR, r.Spec.OverlappingCIDR)
	}
	return validateMirrorPeer(r)
}

// validateMirrorPeer validates the MirrorPeer
func validateMirrorPeer(instance *MirrorPeer) error {
	if instance.Spec.Items == nil {
		return fmt.Errorf("Spec.Items can not be nil")
	}
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *MirrorPeer) ValidateDelete() error {
	mirrorpeerlog.Info("validate delete", "name", r.ObjectMeta.Name)
	return nil
}
