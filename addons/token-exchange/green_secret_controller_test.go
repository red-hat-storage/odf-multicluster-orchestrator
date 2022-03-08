package addons

import (
	"context"
	"fmt"
	"testing"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (fakeSecretHandler) getGreenSecretFilter(obj interface{}) bool {
	if metaObj, has := obj.(metav1.Object); has {
		return metaObj.GetName() == "greensecret"
	}
	return false
}

func (f fakeSecretHandler) syncGreenSecret(name string, namespace string, c *greenSecretTokenExchangeAgentController) error {
	// mocking green secret creation logic to verify params
	secret, err := getSecret(c.hubSecretLister, name, namespace)
	if err != nil {
		return err
	}
	if ok := f.getGreenSecretFilter(secret); !ok {
		return fmt.Errorf("not green secret")
	}
	return nil
}

func TestGreenSecretSync(t *testing.T) {
	cases := []testCase{
		{
			name:          "green secret not found in hub 1 ",
			namespaceName: "ns/wrongname",
			errExpected:   true,
			syncExpected:  false,
		},
		{
			name:          "green secret not found in hub 2",
			namespaceName: "wrongns/test",
			errExpected:   true,
			syncExpected:  false,
		},
		{
			name:          "green secret found in hub",
			namespaceName: "ns/greensecret",
			errExpected:   false,
			syncExpected:  true,
		},
	}
	registerFakeSecretHandler()
	fakeCtrl := getFakeTokenExchangeController(t, utils.DestinationLabel)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := fakeCtrl.Sync(context.TODO(), NewFakeSyncContext(t, c.namespaceName))
			if c.errExpected {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if c.syncExpected {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
