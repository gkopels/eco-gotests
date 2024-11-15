package tests

import (
	"fmt"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/nad"
	"github.com/openshift-kni/eco-goinfra/pkg/pod"
	"github.com/openshift-kni/eco-goinfra/pkg/reportxml"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/internal/coreparams"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/define"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/security/internal/securityenv"

	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/security/internal/securityenv"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/security/internal/tsparams"
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

		By("Verifying if nftables custom firewall tests can be executed on given cluster")
		err := securityenv.DoesClusterSupportNftablesTests(requiredWorkerNodeNumber)

		if err != nil {
			Skip(
				fmt.Sprintf("given cluster is not suitable for nftables tests due to the following "+
					"error %s", err.Error()))
		}

		By("Edit the machineconfiguration cluster to include nftables")
		err = UpdateMachineConfigurationCluster("cluster",
			"nftables.service", "/etc/sysconfig/nftables.conf")
		Expect(err).ToNot(HaveOccurred(), "Failed to update machineconfiguration cluster")

		By("Creating external FRR pod on master node")
		_ = deployTestPods(hubIPv4ExternalAddresses[0])

	})

	AfterAll(func() {

	})

	Context("custom firewall", func() {
		It("Verify the creation of a new custom node firewall nftables table with an ingress rule",
			reportxml.ID("77412"), func() {
				fmt.Println("Hello World")

				By("Create nftables butane file")
				nftablesRules := CreateButaneConfig("worker")
				By("Add nftables custom rule with an ingress rule blocking tcp port 8888")
				machineConfig := CreateNftableCustomRule(nftablesRules)
				fmt.Println("machineConfig.Definition.Name", machineConfig.Definition.Name)
			})
	})

})

func deployTestPods(hubIPv4ExternalAddresses string) *pod.Builder {

	By("Creating External NAD")
	createExternalNad(tsparams.ExternalMacVlanNADName)

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
