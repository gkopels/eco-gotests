package tests

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/namespace"
	"github.com/openshift-kni/eco-goinfra/pkg/nodes"
	"github.com/openshift-kni/eco-goinfra/pkg/pod"
	"github.com/openshift-kni/eco-goinfra/pkg/reportxml"
	"github.com/openshift-kni/eco-goinfra/pkg/sriov"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netenv"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netparam"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/sriov/internal/sriovenv"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/sriov/internal/tsparams"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	srIovPolicyNamePort0     = "sriov-policy-pf-port-0"
	srIovPolicyNamePort1     = "sriov-policy-pf-port-1"
	srIovPolicyResNamePort0  = "sriovpolicypfport0"
	srIovPolicyResNamePort1  = "sriovpolicypfport1"
	srIovPolicyResNameClient = "sriovpolicypfclient"
	srIovNetworkName         = "sriovnetwork-pf-status-relay"
	srIovNetworkDPDKName     = "sriovnetwork-pf-status-relay-dpdk"
	clientLinuxName          = "client-linux-pf-status"
	serverLinuxName          = "server-linux-pf-status"
	clientDPDKName           = "client-dpdk-pf-status"
	serverDPDKName           = "server-dpdk-pf-status"
	perfProfileName          = "performance-profile-pf-status"
)

var (
	workerNodeList           []*nodes.Builder
	srIovInterfacesUnderTest []string
	sriovDeviceID            string
	err                      error
	testCmdLinux             = []string{"bash", "-c", "sleep 5; testcmd -interface net1 -protocol tcp -port 4444 -listen"}
	testCmdDPDK              = []string{"bash", "-c", "sleep 5; testpmd-client -interface net1"}
	pfStatusCheckCMD         = []string{"bash", "-c", "ip link show"}
)

var _ = Describe("PF Status Relay", Ordered, Label(tsparams.LabelSuite), ContinueOnFailure, func() {

	BeforeAll(func() {
		By("Discover worker nodes")
		workerNodeList, err = nodes.List(APIClient,
			metav1.ListOptions{LabelSelector: labels.Set(NetConfig.WorkerLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Failed to discover worker nodes")

		By("Validating SR-IOV interfaces")
		Expect(sriovenv.ValidateSriovInterfaces(workerNodeList, 2)).ToNot(HaveOccurred(),
			"Failed to get required SR-IOV interfaces")

		By("Collecting SR-IOV interfaces for PF status relay testing")
		srIovInterfacesUnderTest, err = NetConfig.GetSriovInterfaces(2)
		Expect(err).ToNot(HaveOccurred(), "Failed to retrieve SR-IOV interfaces for testing")

		By("Verify SR-IOV Device IDs for interface under test")
		sriovDeviceID, err = sriovenv.DiscoverInterfaceUnderTestDeviceID(srIovInterfacesUnderTest[0],
			workerNodeList[0].Definition.Name)
		Expect(err).ToNot(HaveOccurred(), "Failed to discover SR-IOV device ID")
		Expect(sriovDeviceID).ToNot(BeEmpty(), "Expected sriovDeviceID not to be empty")

		By("Verifying if PF status relay tests can be executed on given cluster")
		err = netenv.DoesClusterHasEnoughNodes(APIClient, NetConfig, 2, 1)
		Expect(err).ToNot(HaveOccurred(),
			"Cluster doesn't support PF status relay test cases - requires 2 worker nodes")

		// Pre-test setup steps for PF Status Relay with LACP bonding
		By("Verify SRIOV Operator is running")
		Expect(sriovenv.IsSriovDeployed()).ToNot(HaveOccurred(), "SRIOV Operator is not running")

		By("Verify PF Status Operator is running")
		// TODO: Add PF Status Operator verification function

		By("Create LACP Bond interfaces on worker-0")
		// oc apply -f nncpBond10Worker0.yaml nncpBond20Worker0.yaml
		createLACPBondInterfaces()

		By("Create bond interfaces on the switch with LACP")
		// juniper-commands
		createSwitchBondInterfaces()

		By("Verify LACP is up on the node and the lab switch")
		verifyLACPStatus()

		By("Wait for the cluster to be stable")
		err = netenv.WaitForSriovAndMCPStable(
			APIClient, tsparams.MCOWaitTimeout, time.Minute, NetConfig.CnfMcpLabel, NetConfig.SriovOperatorNamespace)
		Expect(err).ToNot(HaveOccurred(), "Failed while waiting for cluster to be stable")

		By("Deploy PFLACPMonitor CRD monitoring interfaces on worker-0")
		// oc apply -f pflacpmonitor.yaml
		deployPFLACPMonitor()

		By("Verify pfLACPMonitoring pod logs LACP status up")
		verifyPFLACPMonitorStatus()
	})

	AfterEach(func() {
		By("Removing SR-IOV configuration")
		err := netenv.RemoveSriovConfigurationAndWaitForSriovAndMCPStable()
		Expect(err).ToNot(HaveOccurred(), "Failed to remove SR-IOV configuration")

		By("Cleaning test namespace")
		err = namespace.NewBuilder(APIClient, tsparams.TestNamespaceName).CleanObjects(
			netparam.DefaultTimeout, pod.GetGVR())
		Expect(err).ToNot(HaveOccurred(), "Failed to clean test namespace")
	})

	Context("Bond SRIOV client", func() {
		BeforeAll(func() {
			By("Define and create sriov network policies")
			err := sriovenv.DefineCreateSriovNetPolices(srIovPolicyNamePort0, srIovPolicyResNamePort0, srIovInterfacesUnderTest[0],
				sriovDeviceID, "vfio-pci")
			Expect(err).ToNot(HaveOccurred(), "Failed to define and create sriov network policies")

			By("Define and create sriov network policies")
			err = sriovenv.DefineCreateSriovNetPolices(srIovPolicyNamePort1, srIovPolicyResNamePort1, srIovInterfacesUnderTest[1],
				sriovDeviceID, "vfio-pci")
			Expect(err).ToNot(HaveOccurred(), "Failed to define and create sriov network policies")

			By("Define and create sriov network policies")
			err = sriovenv.DefineCreateSriovNetPolices(srIovPolicyNamePort0, srIovPolicyResNamePort0, srIovInterfacesUnderTest[0],
				sriovDeviceID, "vfio-pci")
			Expect(err).ToNot(HaveOccurred(), "Failed to define and create sriov network policies")

			By("Create SriovNetworks for bonded PF interfaces")
			// oc apply -f sriovnetwork-net1.yaml sriovnet-net2.yaml
			createBondedSriovNetworks()

			By("Create Network Attachment Definition for bonded interface")
			// oc apply -f nad-bond.yaml
			createBondNAD()
		})

		It("Verify that a Linux pod with an active-backup bonded interface fails over when the associated VF is "+
			"disabled due to a LACP failure on the node's PF interface", reportxml.ID("83319"), func() {

			By("Create client bond pod using VFs from bonded node interfaces")
			// oc apply -f client-bond.yaml - active-backup bonded interface
			createClientBondPod()

			By("Create client pod on second worker node with sriov interface")
			// oc apply -f client-pod.yaml - same VLAN as bonded client
			createClientPod()

			By("Verify initial connectivity between bonded client and regular client")
			verifyInitialConnectivity()

			By("Simulate LACP failure on one PF interface")
			simulateLACPFailure()

			By("Verify bonded interface fails over to backup interface")
			verifyBondFailover()

			By("Verify connectivity is maintained during failover")
			verifyConnectivityDuringFailover()

			By("Restore LACP on failed interface")
			restoreLACPInterface()

			By("Verify bond returns to primary interface")
			verifyBondRestoration()
		})

		It("Verify PF link down affects VF", reportxml.ID("TBD"), func() {
			By("Testing PF link down scenario")
			// Test implementation placeholder
			glog.V(5).Infof("Test: Verify PF link down affects VF")
		})

		It("Verify PF link up restores VF connectivity", reportxml.ID("TBD"), func() {
			By("Testing PF link up scenario")
			// Test implementation placeholder
			glog.V(5).Infof("Test: Verify PF link up restores VF connectivity")
		})
	})

	Context("DPDK Clients", func() {
		BeforeAll(func() {
			By("Deploying PerformanceProfile is it's not installed")
			err = netenv.DeployPerformanceProfile(
				APIClient,
				NetConfig,
				perfProfileName,
				"1,3,5,7,9,11,13,15,17,19,21,23,25",
				"0,2,4,6,8,10,12,14,16,18,20",
				24)
			Expect(err).ToNot(HaveOccurred(), "Fail to deploy PerformanceProfile")

			definePFStatusRelaySriovNetPolices(srIovPolicyName+"-dpdk", srIovPolicyResName+"dpdk", srIovInterfacesUnderTest[0],
				sriovDeviceID, "vfio-pci")

			By("Creating SR-IOV network for DPDK clients")
			_, err = sriov.NewNetworkBuilder(
				APIClient,
				srIovNetworkDPDKName,
				NetConfig.SriovOperatorNamespace,
				tsparams.TestNamespaceName,
				srIovPolicyResName+"dpdk").Create()
			Expect(err).ToNot(HaveOccurred(), "Failed to create SR-IOV network for DPDK clients")
		})

		It("Verify DPDK application responds to PF status changes", reportxml.ID("TBD"), func() {
			By("Creating DPDK client pod")
			// Test implementation placeholder
			glog.V(5).Infof("Test: Verify DPDK application responds to PF status changes")
		})

		It("Verify DPDK polling mode driver handles PF link state", reportxml.ID("TBD"), func() {
			By("Testing DPDK PMD with PF link state changes")
			// Test implementation placeholder
			glog.V(5).Infof("Test: Verify DPDK polling mode driver handles PF link state")
		})

		It("Verify DPDK packet forwarding stops on PF link down", reportxml.ID("TBD"), func() {
			By("Testing DPDK packet forwarding during PF link down")
			// Test implementation placeholder
			glog.V(5).Infof("Test: Verify DPDK packet forwarding stops on PF link down")
		})
	})
})

// Helper functions for PF Status Relay test setup
func verifyPFInterfacesUp() {
	// TODO: Implement verification that SR-IOV interfaces are connected and up
	glog.V(5).Infof("Verifying PF interfaces are up")
}

func createLACPBondInterfaces() {
	// TODO: Apply nncpBond10Worker0.yaml and nncpBond20Worker0.yaml
	// oc apply -f nncpBond10Worker0.yaml nncpBond20Worker0.yaml
	glog.V(5).Infof("Creating LACP Bond interfaces on worker-0")
}

func createSwitchBondInterfaces() {
	// TODO: Execute juniper commands to create bond interfaces on switch
	glog.V(5).Infof("Creating bond interfaces on switch with LACP")
}

func verifyLACPStatus() {
	// TODO: Verify LACP status on node and switch
	// Node: cat /proc/net/bonding/bond10 and cat /proc/net/bonding/bond20 - expect port state: 63
	// Switch: show lacp interface ae10 and show lacp interface ae20 - expect LACP packets (Active/Current)
	glog.V(5).Infof("Verifying LACP status on node and switch")
}

func createBondedPFPolicies() {
	// TODO: Apply sriovnetworkpolicy-port1.yaml and sriovnetworkpolicy-port2.yaml
	// oc apply -f sriovnetworkpolicy-port1.yaml sriovnetworkpolicy-port2.yaml
	glog.V(5).Infof("Creating SriovNetworkNodePolicys for bonded PF interfaces")
}

func createClientPFPolicy() {
	// TODO: Apply sriovnetworkpolicy-client.yaml
	// oc apply -f sriovnetworkpolicy-client.yaml
	glog.V(5).Infof("Creating SriovNetworkNodePolicy for client pod")
}

func deployPFLACPMonitor() {
	// TODO: Apply pflacpmonitor.yaml
	// oc apply -f pflacpmonitor.yaml
	glog.V(5).Infof("Deploying PFLACPMonitor CRD")
}

func verifyPFLACPMonitorStatus() {
	// TODO: Verify pfLACPMonitoring pod logs show LACP status "up"
	glog.V(5).Infof("Verifying pfLACPMonitoring pod logs")
}

func createBondedSriovNetworks() {
	// TODO: Apply sriovnetwork-net1.yaml and sriovnet-net2.yaml
	// oc apply -f sriovnetwork-net1.yaml sriovnet-net2.yaml
	glog.V(5).Infof("Creating SriovNetworks for bonded PF interfaces")
}

func createBondNAD() {
	// TODO: Apply nad-bond.yaml
	// oc apply -f nad-bond.yaml
	glog.V(5).Infof("Creating Network Attachment Definition for bonded interface")
}

func createClientSriovNetwork() {
	// TODO: Apply sriovnetwork-client.yaml
	// oc apply -f sriovnetwork-client.yaml
	glog.V(5).Infof("Creating SriovNetwork for test client pod")
}

// Test execution helper functions
func createClientBondPod() {
	// TODO: Apply client-bond.yaml - creates pod with active-backup bonded interface using VFs
	// oc apply -f client-bond.yaml
	glog.V(5).Infof("Creating client bond pod with active-backup bonded interface")
}

func createClientPod() {
	// TODO: Apply client-pod.yaml - creates pod on worker-1 with sriov interface in same VLAN
	// oc apply -f client-pod.yaml
	glog.V(5).Infof("Creating client pod on second worker node")
}

func verifyInitialConnectivity() {
	// TODO: Test connectivity between bonded client and regular client
	glog.V(5).Infof("Verifying initial connectivity between pods")
}

func simulateLACPFailure() {
	// TODO: Simulate LACP failure on one PF interface (disable switch port or modify LACP config)
	glog.V(5).Infof("Simulating LACP failure on PF interface")
}

func verifyBondFailover() {
	// TODO: Verify that bonded interface switches to backup interface
	// Check /proc/net/bonding/bond10 for active slave change
	glog.V(5).Infof("Verifying bond failover to backup interface")
}

func verifyConnectivityDuringFailover() {
	// TODO: Verify that connectivity is maintained during the failover
	glog.V(5).Infof("Verifying connectivity is maintained during failover")
}

func restoreLACPInterface() {
	// TODO: Restore LACP on the failed interface (re-enable switch port)
	glog.V(5).Infof("Restoring LACP on failed interface")
}

func verifyBondRestoration() {
	// TODO: Verify bond returns to primary interface when LACP is restored
	glog.V(5).Infof("Verifying bond restoration to primary interface")
}

// definePFStatusRelaySriovNetPolices creates SR-IOV policies for PF Status Relay tests
// without waiting for cluster stability (handled separately in test)
func definePFStatusRelaySriovNetPolices(vfioPCIName, vfioPCIResName, sriovInterface,
	sriovDeviceID, reqDriver string) {
	By("Define and create sriov network policy using worker node label with netDevice type " + reqDriver)

	err := sriovenv.DefineCreateSriovNetPolices(vfioPCIName, vfioPCIResName, sriovInterface,
		sriovDeviceID, reqDriver)
	Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Failed to create sriov network policy %s", vfioPCIName))

	By("Waiting for SR-IOV and MCP to be stable after policy creation")
	err = netenv.WaitForSriovAndMCPStable(
		APIClient, tsparams.MCOWaitTimeout, time.Minute, NetConfig.CnfMcpLabel, NetConfig.SriovOperatorNamespace)
	Expect(err).ToNot(HaveOccurred(), "Failed while waiting for SR-IOV to be stable")
}
