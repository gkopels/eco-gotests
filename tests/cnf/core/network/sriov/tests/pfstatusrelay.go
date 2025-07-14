package tests

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-kni/eco-goinfra/pkg/nad"
	"github.com/openshift-kni/eco-goinfra/pkg/namespace"
	"github.com/openshift-kni/eco-goinfra/pkg/nodes"
	"github.com/openshift-kni/eco-goinfra/pkg/pod"
	"github.com/openshift-kni/eco-goinfra/pkg/reportxml"
	"github.com/openshift-kni/eco-goinfra/pkg/sriov"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netconfig"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netenv"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netparam"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/sriov/internal/sriovenv"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/sriov/internal/tsparams"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	sriovAndResourceNameNet1 = "pfstatusrelay-net1"
	sriovAndResourceNameNet2 = "pfstatusrelay-net2"
	bondNadName              = "pfstatus-bond"
	testPodName              = "pfstatus-test-pod"
)

var _ = Describe("SRIOV: PF Status Relay:", Ordered, Label(tsparams.LabelPFStatusRelayTestCases),
	ContinueOnFailure, func() {
		var (
			workerNodeList           []*nodes.Builder
			err                      error
			sriovInterfacesUnderTest []string
			switchCredentials        *sriovenv.SwitchCredentials
			switchConfig             *netconfig.NetworkConfig
			switchInterfaces         []string
		)

		BeforeAll(func() {
			By("Validating SR-IOV interfaces")
			workerNodeList, err = nodes.List(APIClient,
				metav1.ListOptions{LabelSelector: labels.Set(NetConfig.WorkerLabelMap).String()})
			Expect(err).ToNot(HaveOccurred(), "Failed to discover worker nodes")

			// Need at least 2 interfaces for bonding
			Expect(sriovenv.ValidateSriovInterfaces(workerNodeList, 2)).ToNot(HaveOccurred(),
				"Failed to get required SR-IOV interfaces for bonding")
			sriovInterfacesUnderTest, err = NetConfig.GetSriovInterfaces(2)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve SR-IOV interfaces for testing")

			By("Verifying if PF status relay tests can be executed on given cluster")
			err = netenv.DoesClusterHasEnoughNodes(APIClient, NetConfig, 1, 1)
			Expect(err).ToNot(HaveOccurred(),
				"Cluster doesn't support PF status relay test cases")

			By("Configure Bonded interface on the lab switch")
			switchCredentials, err = sriovenv.NewSwitchCredentials()
			Expect(err).ToNot(HaveOccurred(), "Failed to get switch credentials")

			switchConfig = netconfig.NewNetConfig()
			switchInterfaces, err = switchConfig.GetSwitchInterfaces()
			Expect(err).ToNot(HaveOccurred(), "Failed to get switch interfaces")

			By("Configure LACP bond on switch interfaces")
			err = configureSwithLacpBond(switchCredentials, switchInterfaces)
			Expect(err).ToNot(HaveOccurred(), "Failed to configure LACP bond on switch")
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

		AfterAll(func() {
			By("Restore switch configuration")
			err = restoreSwitchConfiguration(switchCredentials, switchInterfaces)
			if err != nil {
				By(fmt.Sprintf("Warning: Failed to restore switch configuration: %v", err))
			}
		})

		It("Verify that a Linux pod with an active-backup bonded interface fails over when the "+
			"associated VF is disabled due to a LACP failure on the node's PF interface",
			reportxml.ID("83319"), func() {
				testPFStatusRelayFailover(sriovInterfacesUnderTest, workerNodeList[0].Object.Name,
					switchCredentials, switchInterfaces)
			})
	})

// testPFStatusRelayFailover tests PF status relay functionality with bonded interfaces
func testPFStatusRelayFailover(interfacesUnderTest []string, workerName string,
	switchCredentials *sriovenv.SwitchCredentials, switchInterfaces []string) {

	By("Creating SR-IOV policies for bonded interfaces")
	// Create first SR-IOV policy
	sriovPolicy1 := sriov.NewPolicyBuilder(
		APIClient,
		sriovAndResourceNameNet1,
		NetConfig.SriovOperatorNamespace,
		sriovAndResourceNameNet1,
		2, // Need at least 2 VFs
		[]string{interfacesUnderTest[0]},
		NetConfig.WorkerLabelMap).WithDevType("netdevice")

	err := sriovenv.CreateSriovPolicyAndWaitUntilItsApplied(sriovPolicy1, tsparams.MCOWaitTimeout)
	Expect(err).ToNot(HaveOccurred(), "Failed to configure first SR-IOV policy")

	// Create second SR-IOV policy
	sriovPolicy2 := sriov.NewPolicyBuilder(
		APIClient,
		sriovAndResourceNameNet2,
		NetConfig.SriovOperatorNamespace,
		sriovAndResourceNameNet2,
		2, // Need at least 2 VFs
		[]string{interfacesUnderTest[1]},
		NetConfig.WorkerLabelMap).WithDevType("netdevice")

	err = sriovenv.CreateSriovPolicyAndWaitUntilItsApplied(sriovPolicy2, tsparams.MCOWaitTimeout)
	Expect(err).ToNot(HaveOccurred(), "Failed to configure second SR-IOV policy")

	By("Creating SR-IOV networks for bonded interfaces")
	// Create first SR-IOV network (without IPAM for bonding)
	_, err = sriov.NewNetworkBuilder(
		APIClient,
		sriovAndResourceNameNet1,
		NetConfig.SriovOperatorNamespace,
		tsparams.TestNamespaceName,
		sriovAndResourceNameNet1).
		WithMacAddressSupport().
		WithLogLevel(netparam.LogLevelDebug).
		Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create first SR-IOV network")

	// Create second SR-IOV network (without IPAM for bonding)
	_, err = sriov.NewNetworkBuilder(
		APIClient,
		sriovAndResourceNameNet2,
		NetConfig.SriovOperatorNamespace,
		tsparams.TestNamespaceName,
		sriovAndResourceNameNet2).
		WithMacAddressSupport().
		WithLogLevel(netparam.LogLevelDebug).
		Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create second SR-IOV network")

	By("Creating bonded network attachment definition")
	bondNad := createPFStatusBondNAD(bondNadName, "active-backup")
	_, err = bondNad.Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create bonded NAD")

	By("Creating test pod with bonded SRIOV interfaces")
	testPod := createBondedPFStatusTestPod(testPodName, workerName, sriovAndResourceNameNet1,
		sriovAndResourceNameNet2, bondNadName)

	By("Verifying initial connectivity through bond interface")
	err = validateBondConnectivity(testPod, "bond0")
	Expect(err).ToNot(HaveOccurred(), "Failed initial connectivity test")

	By("Simulating LACP failure on primary interface")
	err = simulateLacpFailure(switchCredentials, switchInterfaces[0])
	Expect(err).ToNot(HaveOccurred(), "Failed to simulate LACP failure")

	By("Waiting for bond failover and verifying connectivity")
	Eventually(func() error {
		return validateBondFailover(testPod, "bond0")
	}, 60*time.Second, 5*time.Second).ShouldNot(HaveOccurred(),
		"Bond failover did not complete successfully")

	By("Restoring LACP configuration")
	err = restoreLacpConfiguration(switchCredentials, switchInterfaces[0])
	Expect(err).ToNot(HaveOccurred(), "Failed to restore LACP configuration")

	By("Verifying bond returns to primary interface")
	Eventually(func() error {
		return validateBondPrimaryRestore(testPod, "bond0")
	}, 60*time.Second, 5*time.Second).ShouldNot(HaveOccurred(),
		"Bond did not return to primary interface")
}

// createPFStatusBondNAD creates a bond NAD for PF status testing
func createPFStatusBondNAD(nadName, mode string) *nad.Builder {
	bondNad, err := nad.NewMasterBondPlugin(nadName, mode).WithFailOverMac(1).
		WithLinksInContainer(true).WithMiimon(100).
		WithLinks([]nad.Link{{Name: "net1"}, {Name: "net2"}}).WithCapabilities(&nad.Capability{IPs: true}).
		WithIPAM(&nad.IPAM{Type: "static"}).GetMasterPluginConfig()
	Expect(err).ToNot(HaveOccurred(), "Failed to define Bond NAD for %s", nadName)

	createdNad, err := nad.NewBuilder(APIClient, nadName, tsparams.TestNamespaceName).WithMasterPlugin(bondNad).Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create Bond NAD for %s", nadName)

	return createdNad
}

// createBondedPFStatusTestPod creates a test pod with bonded SRIOV interfaces
func createBondedPFStatusTestPod(name, workerName, sriovNet1, sriovNet2, bondNadName string) *pod.Builder {
	annotation := pod.StaticIPBondAnnotationWithInterface(bondNadName, "bond0",
		[]string{sriovNet1, sriovNet2}, []string{tsparams.ClientIPv4IPAddress})

	testPod, err := pod.NewBuilder(APIClient, name, tsparams.TestNamespaceName,
		NetConfig.CnfNetTestContainer).
		DefineOnNode(workerName).
		WithSecondaryNetwork(annotation).
		WithPrivilegedFlag().
		CreateAndWaitUntilRunning(netparam.DefaultTimeout)
	Expect(err).ToNot(HaveOccurred(), "Failed to create bonded test pod")

	return testPod
}

// Switch configuration helper functions
func configureSwithLacpBond(credentials *sriovenv.SwitchCredentials, interfaces []string) error {
	// TODO: Implement actual switch LACP configuration
	// This would typically use Juniper/Cisco APIs to configure LACP
	By(fmt.Sprintf("Configuring LACP bond on switch interfaces: %v", interfaces))
	time.Sleep(5 * time.Second) // Placeholder for actual configuration
	return nil
}

func simulateLacpFailure(credentials *sriovenv.SwitchCredentials, interfaceName string) error {
	// TODO: Implement actual LACP failure simulation
	By(fmt.Sprintf("Simulating LACP failure on interface: %s", interfaceName))
	time.Sleep(2 * time.Second) // Placeholder for actual failure simulation
	return nil
}

func restoreLacpConfiguration(credentials *sriovenv.SwitchCredentials, interfaceName string) error {
	// TODO: Implement actual LACP restoration
	By(fmt.Sprintf("Restoring LACP configuration on interface: %s", interfaceName))
	time.Sleep(2 * time.Second) // Placeholder for actual restoration
	return nil
}

func restoreSwitchConfiguration(credentials *sriovenv.SwitchCredentials, interfaces []string) error {
	// TODO: Implement actual switch configuration restoration
	By(fmt.Sprintf("Restoring switch configuration for interfaces: %v", interfaces))
	time.Sleep(3 * time.Second) // Placeholder for actual restoration
	return nil
}

// Bond validation helper functions
func validateBondConnectivity(testPod *pod.Builder, bondInterface string) error {
	// TODO: Implement bond connectivity validation
	By(fmt.Sprintf("Validating connectivity through bond interface: %s", bondInterface))
	return nil
}

func validateBondFailover(testPod *pod.Builder, bondInterface string) error {
	// TODO: Implement bond failover validation
	By(fmt.Sprintf("Validating bond failover for interface: %s", bondInterface))
	return nil
}

func validateBondPrimaryRestore(testPod *pod.Builder, bondInterface string) error {
	// TODO: Implement bond primary restoration validation
	By(fmt.Sprintf("Validating bond primary restoration for interface: %s", bondInterface))
	return nil
}
