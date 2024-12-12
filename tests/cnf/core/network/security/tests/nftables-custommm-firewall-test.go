package tests

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/nad"
	"github.com/openshift-kni/eco-goinfra/pkg/pod"
	"github.com/openshift-kni/eco-goinfra/pkg/reportxml"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/internal/coreparams"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/define"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/security/internal/tsparams"
	ocpoperatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	hubIPv4ExternalAddresses = []string{"172.16.0.10", "172.16.0.11"}
	externalNad              *nad.Builder
)

const (
	requiredWorkerNodeNumber = 2
)

var _ = Describe("nftables", Ordered, Label(tsparams.LabelNftablesTestCases), ContinueOnFailure, func() {

	BeforeAll(func() {

		//By("Verifying if nftables custom firewall tests can be executed on given cluster")
		//
		//err := securityenv.DoesClusterSupportNftablesTests(requiredWorkerNodeNumber)
		//
		//if err != nil {
		//	Skip(
		//		fmt.Sprintf("given cluster is not suitable for nftables tests due to the following "+
		//			"error %s", err.Error()))
		//}

		By("Edit the machineconfiguration cluster to include nftables")
		updateMachineConfigurationCluster(true)

		By("Creating external FRR pod on master node")
		//_ = deployTestPods(hubIPv4ExternalAddresses[0])

	})

	AfterAll(func() {
		By("Edit the machineconfiguration cluster to remove nftables")
		updateMachineConfigurationCluster(false)
	})

	Context("custom firewall", func() {
		It("Verify the creation of a new custom node firewall nftables table with an ingress rule",
			reportxml.ID("77412"), func() {
				fmt.Println("Hello World")

				//By("Create nftables butane file")
				//mcConfig, err := CreateMC("worker", "/tmp/greg/")
				//Expect(err).ToNot(HaveOccurred(), "Failed to create nftables rules")
				//err = ApplyMachineConfig(mcConfig)
				//Expect(err).ToNot(HaveOccurred(), "Failed to create machine config")
			})
	})

})

func deployTestPods(hubIPv4ExternalAddresses string) *pod.Builder {

	By("Creating External NAD")
	// createExternalNad(tsparams.ExternalMacVlanNADName)

	By("Creating static ip annotation")

	By("Creating FRR Pod")

	return nil
}

func createExternalNad(name string) {
	var externalNad *nad.Builder
	By("Creating external BR-EX NetworkAttachmentDefinition")
	macVlanPlugin, err := define.MasterNadPlugin(coreparams.OvnExternalBridge, "bridge", nad.IPAMStatic())
	Expect(err).ToNot(HaveOccurred(), "Failed to define master nad plugin")
	externalNad, err = nad.NewBuilder(APIClient, name, tsparams.TestNamespaceName).
		WithMasterPlugin(macVlanPlugin).Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create external NetworkAttachmentDefinition")
	Expect(externalNad.Exists()).To(BeTrue(), "Failed to detect external NetworkAttachmentDefinition")
}

func updateMachineConfigurationCluster(nftables bool) {
	By("should have MetalLB controller in running state")
	var jsonBytes []byte

	if nftables {
		jsonBytes = []byte(`
    {"spec":{"nodeDisruptionPolicy":
       {"files": [{"actions":
    [{"restart": {"serviceName": "nftables.service"},"type": "Restart"}],
    "path": "/etc/sysconfig/nftables.conf"}],
    "units":
    [{"actions":
    [{"reload": {"serviceName":"nftables.service"},"type": "Reload"},
    {"type": "DaemonReload"}],"name": "nftables.service"}]}}}`)
	} else {
		jsonBytes = []byte(`{"spec":{
    "nodeDisruptionPolicy": {"files": [],"units": []}
    }}`)

	}

	err := APIClient.Patch(context.TODO(), &ocpoperatorv1.MachineConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	}, client.RawPatch(types.MergePatchType, jsonBytes))
	if err != nil {
		fmt.Println(err)
	}
}
