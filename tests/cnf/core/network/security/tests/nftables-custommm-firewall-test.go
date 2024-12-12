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

	Context("custom firewall", func() {
		It("Verify the creation of a new custom node firewall nftables table with an ingress rule",
			reportxml.ID("77412"), func() {
				fmt.Println("Hello World")

				By("Create nftables butane file")
				mcConfig, err := CreateMC("worker", "/tmp/greg/")
				Expect(err).ToNot(HaveOccurred(), "Failed to create nftables rules")
				err = ApplyMachineConfig(mcConfig)
				Expect(err).ToNot(HaveOccurred(), "Failed to create machine config")
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
