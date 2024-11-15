package tsparams

import (
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netparam"
	"github.com/openshift-kni/k8sreporter"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// Labels represents the range of labels that can be used for test cases selection.
	Labels = append(netparam.Labels, LabelSuite)

	// ReporterNamespacesToDump tells to the reporter from where to collect logs.
	ReporterNamespacesToDump = map[string]string{
		"openshift-performance-addon-operator": "performance",
	}

	// TestNamespaceName metalLb namespace where all test cases are performed.
	TestNamespaceName = "security-tests"
	// OperatorControllerManager defaults machine-config daemonset controller name.
	OperatorControllerManager = "machine-config-controller"
	// ReporterCRDsToDump tells to the reporter what CRs to dump.
	ReporterCRDsToDump = []k8sreporter.CRData{}
	// ExternalMacVlanNADName represents default external NetworkAttachmentDefinition name.
	ExternalMacVlanNADName = "external"
)

func setUnstructured(kind string) *unstructured.UnstructuredList {
	resource := &unstructured.UnstructuredList{}

	gvk := schema.GroupVersionKind{
		//Group:   metallb.APIGroup,
		//Version: metallb.APIVersion,
		Kind: kind,
	}

	resource.SetGroupVersionKind(gvk)

	return resource
}
