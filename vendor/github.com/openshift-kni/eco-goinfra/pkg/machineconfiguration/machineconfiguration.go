package machineconfiguration

import (
	"github.com/golang/glog"
	"github.com/openshift-kni/eco-goinfra/pkg/clients"
	apioperatorv1 "github.com/openshift/api/operator/v1"
	operatorv1 "github.com/openshift/client-go/operator/applyconfigurations/operator/v1"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type MachineConfigurationBuilder struct {
	// MachineConfig definition. Used to create MachineConfig object with minimum set of required elements.
	Definition *operatorv1.MachineConfigurationApplyConfiguration
	// Created MachineConfig object on the cluster.
	Object *operatorv1.MachineConfigurationApplyConfiguration
	// api client to interact with the cluster.
	apiClient runtimeclient.Client
	// errorMsg is processed before MachineConfig object is created.
	errorMsg string
}

func NewMacchineConfigurationBuilder(apiClient *clients.Settings, name string) *MachineConfigurationBuilder {
	glog.V(100).Infof("Initializing new NewMacchineConfigurationBuilder structure with following params: %s", name)

	if apiClient == nil {
		glog.V(100).Info("The apiClient of the machineconfiguration is nil")

		return nil
	}

	err := apiClient.AttachScheme(apioperatorv1.Install)
	if err != nil {
		glog.V(100).Info("Failed to add machineconfiguration v1 scheme to client schemes")

		return nil
	}

	builder := MachineConfigurationBuilder{
		apiClient: apiClient.Client,
		Definition: &operatorv1.MachineConfigurationApplyConfiguration{
			ObjectMetaApplyConfiguration: &applymetav1.ObjectMetaApplyConfiguration{
				Name: &name,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("The name of the machineconfiguration is empty")

		builder.errorMsg = "machineconfiguration 'name' cannot be empty"
	}

	return &builder
}
