package securityenv

import (
	"github.com/golang/glog"
	"github.com/openshift-kni/eco-goinfra/pkg/clients"

	machineconfigurationv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Builder provides struct for a MachineConfiguration object
type Builder struct {
	Definition *machineconfigurationv1.MachineConfiguration
	Object     *machineconfigurationv1.MachineConfiguration
	apiClient  runtimeClient.Client
	errorMsg   string
}

func NewBuilder() *Builder {
	return &Builder{}
}

func Pull(apiClient *clients.Settings, name string) (b *Builder, err error) {
	glog.V(100).Infof("Pulling existing machineSet name %s ", name)

	err = apiClient.AttachScheme(machineconfigurationv1.AddToScheme)
	if err != nil {
		glog.V(100).Infof("Failed to add machineconfiguration_remove scheme to client schemes")

		return nil, err
	}

	builder := Builder{
		apiClient: apiClient.Client,
		Definition: &machineconfigurationv1.MachineConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
	}

	builder.Definition = builder.Object

	return &Builder{}, nil
}
