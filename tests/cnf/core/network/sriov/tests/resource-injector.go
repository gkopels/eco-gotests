package tests

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/namespace"
	"github.com/openshift-kni/eco-goinfra/pkg/nodes"
	"github.com/openshift-kni/eco-goinfra/pkg/pod"
	"github.com/openshift-kni/eco-goinfra/pkg/reportxml"
	"github.com/openshift-kni/eco-goinfra/pkg/sriov"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netenv"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/sriov/internal/tsparams"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var _ = Describe("resource-injector", Ordered, Label(tsparams.LabelSuite), ContinueOnFailure, func() {

	BeforeAll(func() {
		By("Discover worker nodes")
		var err error

		workerNodes, err = nodes.List(APIClient,
			metav1.ListOptions{LabelSelector: labels.Set(NetConfig.WorkerLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Fail to discover nodes")

		By("Collecting SR-IOV interfaces for testing")
		srIovInterfacesUnderTest, err := NetConfig.GetSriovInterfaces(1)
		Expect(err).ToNot(HaveOccurred(), "Failed to retrieve SR-IOV interfaces for testing")

		By(fmt.Sprintf("Define and create sriov network policy on %s", workerNodes[0].Definition.Name))
		nodeSelectorWorker0 := map[string]string{
			"kubernetes.io/hostname": workerNodes[0].Definition.Name,
		}

		_, err = sriov.NewPolicyBuilder(
			APIClient,
			srIovPolicyNode1Name,
			NetConfig.SriovOperatorNamespace,
			srIovPolicyNode0ResName,
			6,
			[]string{fmt.Sprintf("%s#0-5", srIovInterfacesUnderTest[0])},
			nodeSelectorWorker0).WithMTU(9000).WithVhostNet(true).Create()
		Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Failed to create an sriov policy on %s",
			workerNodes[0].Definition.Name))

		By("Define and create sriov network")
		defineAndCreateSrIovNetwork(srIovNetworkDefaultNode1, srIovPolicyNode0ResName, true)

		By("Waiting until cluster MCP and SR-IOV are stable")
		err = netenv.WaitForSriovAndMCPStable(
			APIClient, tsparams.MCOWaitTimeout, time.Minute, NetConfig.CnfMcpLabel, NetConfig.SriovOperatorNamespace)
		Expect(err).ToNot(HaveOccurred(), "Failed cluster is not stable")
	})

	It("Validate a pod can receive non-member multicast IPv6 traffic over a secondary SRIOV interface"+
		" when allmulti mode is enabled from a multicast source in the same PF", reportxml.ID("67813"), func() {
		multicastServer := createMulticastServer(srIovNetworkDefaultNode1, multicastServerIPv6Mac,
			[]string{multicastServerIPv6}, multicastPingIPv6CMD, workerNodes[0].Definition.Name)

		defaultClient := createTestClient(clientDefaultName, srIovNetworkDefaultNode1,
			clientAllmultiDisabledIPv6Mac, workerNodes[0].Definition.Name, []string{clientAllmultiDisabledIPv6})

		allMultiEnabledClient := createTestClient(clientAllmultiEnabledName, srIovNetworkAllMultiNode1,
			clientAllmultiEnabledIPv6Mac, workerNodes[0].Definition.Name, []string{clientAllmultiEnabledIPv6})

		runAllMultiTestCases(multicastServer, defaultClient, allMultiEnabledClient, multicastServerIPv6,
			clientAllmultiDisabledIPv6, multicastIPv6GroupIP, addIPv6MCGroupMacCMD)
	})

	AfterEach(func() {
		By("Removing all pods from test namespace")
		runningNamespace, err := namespace.Pull(APIClient, tsparams.TestNamespaceName)
		Expect(err).ToNot(HaveOccurred(), "Failed to pull namespace")
		Expect(runningNamespace.CleanObjects(
			tsparams.WaitTimeout, pod.GetGVR())).ToNot(HaveOccurred())

	})

	AfterAll(func() {
		By("Removing all SR-IOV Policy")
		err := sriov.CleanAllNetworkNodePolicies(APIClient, NetConfig.SriovOperatorNamespace)
		Expect(err).ToNot(HaveOccurred(), "Failed to clean srIovPolicy")

		By("Removing all srIovNetworks")
		err = sriov.CleanAllNetworksByTargetNamespace(
			APIClient, NetConfig.SriovOperatorNamespace, tsparams.TestNamespaceName)
		Expect(err).ToNot(HaveOccurred(), "Failed to clean sriov networks")
	})
})
