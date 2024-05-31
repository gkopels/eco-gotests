package tests

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/openshift-kni/eco-goinfra/pkg/clients"
	"github.com/openshift-kni/eco-goinfra/pkg/configmap"
	"github.com/openshift-kni/eco-goinfra/pkg/mco"
	"github.com/openshift-kni/eco-goinfra/pkg/nto"
	"github.com/openshift-kni/eco-gotests/tests/internal/cluster"
	v2 "github.com/openshift/cluster-node-tuning-operator/pkg/apis/performanceprofile/v2"
	corev1 "k8s.io/api/core/v1"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/openshift-kni/eco-goinfra/pkg/nad"
	"github.com/openshift-kni/eco-goinfra/pkg/reportxml"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/cmd"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/define"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netconfig"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netparam"
	multus "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/namespace"
	"github.com/openshift-kni/eco-goinfra/pkg/nodes"
	"github.com/openshift-kni/eco-goinfra/pkg/pod"
	"github.com/openshift-kni/eco-goinfra/pkg/sriov"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netenv"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/sriov/internal/sriovenv"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/sriov/internal/tsparams"
	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var _ = Describe("QinQ", Ordered, Label(tsparams.LabelQinQTestCases), ContinueOnFailure, func() {

	var (
		err                         error
		dot1ad                      = "802.1ad"
		dot1q                       = "802.1q"
		srIovPolicyNetDevice        = "sriovpolicy-netdevice"
		srIovPolicyVfioPci          = "sriovpolicy-vfiopci"
		srIovPolicyResNameNetDevice = "sriovpolicynetdevice"
		srIovPolicyResNameVfioPci   = "sriovpolicyvfiopci"
		srIovNetworkDot1AD          = "sriovnetwork-dot1ad"
		srIovNetworkDPDKDot1AD      = "sriovnetwork-dpdk-dot1ad"
		srIovNetworkDPDKDot1Q       = "sriovnetwork-dpdk-dot1q"
		srIovNetworkDot1Q           = "sriovnetwork-dot1q"
		srIovNetworkPromiscuous     = "sriovnetwork-promiscuous"
		serverNameDot1ad            = "server-1ad"
		serverNameDPDKDot1ad        = "server-dpdk-1ad"
		serverNameDot1q             = "server-1q"
		clientNameDot1ad            = "client-1ad"
		clientNameDot1q             = "client-1q"
		dpdkServerMac               = "60:00:00:00:00:02"
		dpdkClientMac               = "60:00:00:00:00:01"
		nadCVLAN100                 = "nadcvlan100"
		nadCVLANDpdk                = "nadcvlandpdk"
		intelDeviceIDE810           = "1593"
		mlxDevice                   = "1017"
		tcpDumpNet1CMD              = []string{"bash", "-c", "tcpdump -i net1 -e > /tmp/tcpdump"}
		tcpDumpReadFileCMD          = []string{"bash", "-c", "tail -20 /tmp/tcpdump"}
		tcpDumpDot1ADOutput         = "(ethertype 802\\.1Q-QinQ \\(0x88a8\\)).*?(ethertype 802\\.1Q, vlan 100)"
		tcpDumpDot1QOutput          = "(ethertype 802\\.1Q \\(0x8100\\)).*?(ethertype 802\\.1Q, vlan 100)"
		workerNodeList              = []*nodes.Builder{}
		promiscVFCommand            string
		srIovInterfacesUnderTest    []string
		sriovDeviceID               string
		switchCredentials           *sriovenv.SwitchCredentials
		switchConfig                *netconfig.NetworkConfig
		switchInterfaces            []string
	)

	serverIPV4IP, _, _ := net.ParseCIDR(tsparams.ServerIPv4IPAddress)
	serverIPV6IP, _, _ := net.ParseCIDR(tsparams.ServerIPv6IPAddress)

	BeforeAll(func() {
		By("Discover worker nodes")
		workerNodeList, err = nodes.List(APIClient,
			metav1.ListOptions{LabelSelector: labels.Set(NetConfig.WorkerLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Fail to discover worker nodes")

		By("Collecting SR-IOV interfaces for qinq testing")
		srIovInterfacesUnderTest, err = NetConfig.GetSriovInterfaces(1)
		Expect(err).ToNot(HaveOccurred(), "Failed to retrieve SR-IOV interfaces for testing")

		By("Define and create a network attachment definition with a C-VLAN 100")
		_, err = define.VlanNad(APIClient, nadCVLAN100, tsparams.TestNamespaceName, "net1", 100,
			nad.IPAMStatic())
		Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Fail to create Network-Attachment-Definition %s",
			nadCVLAN100))

		By("Verify SR-IOV Device IDs for interface under test")
		sriovDeviceID = discoverInterfaceUnderTestDeviceID(srIovInterfacesUnderTest[0],
			workerNodeList[0].Definition.Name)
		Expect(sriovDeviceID).ToNot(BeEmpty(), "Expected sriovDeviceID not to be empty")

		By("Define and create sriov network with netDevice type netwdevice")
		_, err = sriov.NewPolicyBuilder(
			APIClient,
			srIovPolicyNetDevice,
			NetConfig.SriovOperatorNamespace,
			srIovPolicyResNameNetDevice,
			10,
			[]string{fmt.Sprintf("%s#0-9", srIovInterfacesUnderTest[0])},
			NetConfig.WorkerLabelMap).Create()
		Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Failed to create sriovnetwork policy %s",
			srIovPolicyNetDevice))

		By("Define and create sriov network with netDevice type vfio-pci")
		sriovPolicy := sriov.NewPolicyBuilder(
			APIClient,
			srIovPolicyVfioPci,
			NetConfig.SriovOperatorNamespace,
			srIovPolicyResNameVfioPci,
			16,
			[]string{fmt.Sprintf("%s#10-15", srIovInterfacesUnderTest[0])},
			NetConfig.WorkerLabelMap).WithDevType("vfio-pci")
		if sriovDeviceID == mlxDevice {
			_, err = sriovPolicy.WithRDMA(true).Create()
		} else {
			_, err = sriovPolicy.Create()
		}
		Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Failed to create sriovnetwork policy %s",
			srIovPolicyNetDevice))

		By("Waiting until cluster MCP and SR-IOV are stable")
		err = netenv.WaitForSriovAndMCPStable(
			APIClient, tsparams.MCOWaitTimeout, time.Minute, NetConfig.CnfMcpLabel, NetConfig.SriovOperatorNamespace)
		Expect(err).ToNot(HaveOccurred(), "Failed cluster is not stable")

		By("Define and create sriov-network for the promiscuous client")
		_, err = sriov.NewNetworkBuilder(APIClient,
			srIovNetworkPromiscuous, NetConfig.SriovOperatorNamespace, tsparams.TestNamespaceName,
			srIovPolicyResNameNetDevice).WithTrustFlag(true).Create()
		Expect(err).ToNot(HaveOccurred(),
			fmt.Sprintf("Failed to create sriov network srIovNetworkPromiscuous %s", err))

		promiscVFCommand = fmt.Sprintf("ethtool --set-priv-flags %s vf-true-promisc-support on",
			srIovInterfacesUnderTest[0])
		if sriovDeviceID == mlxDevice {
			promiscVFCommand = fmt.Sprintf("ip link set %s promisc on",
				srIovInterfacesUnderTest[0])
		}

		By(fmt.Sprintf("Enable VF promiscuous support on %s", srIovInterfacesUnderTest[0]))
		output, err := cmd.RunCommandOnHostNetworkPod(workerNodeList[0].Definition.Name, NetConfig.SriovOperatorNamespace,
			promiscVFCommand)
		Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Failed to run command on node %s", output))

		By("Configure lab switch interface to support VLAN double tagging")
		switchCredentials, err = sriovenv.NewSwitchCredentials()
		Expect(err).ToNot(HaveOccurred(), "Failed to get switch credentials")

		switchConfig = netconfig.NewNetConfig()
		switchInterfaces, err = switchConfig.GetSwitchInterfaces()
		Expect(err).ToNot(HaveOccurred(), "Failed to get switch interfaces")

		err = enableDot1ADonSwitchInterfaces(switchCredentials, switchInterfaces)
		Expect(err).ToNot(HaveOccurred(), "Failed to enable 802.1AD on the switch")
	})

	Context("802.1AD", func() {
		BeforeAll(func() {
			By("Verify SR-IOV Device IDs for interface under test")

			if sriovDeviceID != intelDeviceIDE810 {
				Skip(fmt.Sprintf("The NIC %s does not support 802.1AD", sriovDeviceID))
			}

			By("Define and create sriov-network with 802.1ad S-VLAN")
			vlan, err := strconv.Atoi(NetConfig.VLAN)
			Expect(err).ToNot(HaveOccurred(), "Failed to convert VLAN value")
			defineAndCreateSrIovNetworkWithQinQ(srIovNetworkDot1AD, srIovPolicyResNameNetDevice, dot1ad,
				uint16(vlan))
		})

		It("Verify network traffic over a 802.1ad QinQ tunnel between two SRIOV pods on the same PF",
			reportxml.ID("71676"), func() {
				By("Define and create a container in promiscuous mode")
				tcpDumpContainer := createPromiscuousClient(workerNodeList[0].Definition.Name,
					tcpDumpNet1CMD)

				By("Define and create a server container")
				serverPod := createServerTestPod(serverNameDot1ad, srIovNetworkDot1AD, nadCVLAN100,
					workerNodeList[0].Definition.Name, []string{tsparams.ServerIPv4IPAddress,
						tsparams.ServerIPv6IPAddress})

				By("Define and create a client container")
				clientPod := createClientTestPod(clientNameDot1ad, srIovNetworkDot1AD, nadCVLAN100,
					workerNodeList[0].Definition.Name, []string{tsparams.ClientIPv4IPAddress, tsparams.ClientIPv6IPAddress})

				By("Validate IPv4 and IPv6 connectivity between the containers over the qinq tunnel.")
				err = cmd.ICMPConnectivityCheck(serverPod,
					[]string{tsparams.ClientIPv4IPAddress, tsparams.ClientIPv6IPAddress}, "net2")
				Expect(err).ToNot(HaveOccurred(),
					"Failed to ping the client container over the 802.1AD connection")

				By("Validate IPv4 tcp traffic and dot1ad encapsulation from the client to server.")
				validateTCPTraffic(clientPod, serverIPV4IP.String())

				By("Validate IPv6 tcp traffic and dot1ad encapsulation from the client to server.")
				validateTCPTraffic(clientPod, serverIPV6IP.String())

				By("Validate that the TCP traffic is double tagged")
				readAndValidateTCPDump(tcpDumpContainer, tcpDumpReadFileCMD, tcpDumpDot1ADOutput)
			})

		It("Verify network traffic over a 802.1ad QinQ tunnel between two SRIOV containers in different nodes",
			reportxml.ID("71678"), func() {
				By("Define and create a container in promiscuous mode")
				tcpDumpContainer := createPromiscuousClient(workerNodeList[0].Definition.Name,
					tcpDumpNet1CMD)

				By("Define and create a server container")
				serverPod := createServerTestPod(serverNameDot1ad, srIovNetworkDot1AD, nadCVLAN100,
					workerNodeList[1].Definition.Name, []string{tsparams.ServerIPv4IPAddress, tsparams.ServerIPv6IPAddress})

				By("Define and create a client container")
				clientPod := createClientTestPod(clientNameDot1ad, srIovNetworkDot1AD, nadCVLAN100,
					workerNodeList[0].Definition.Name, []string{tsparams.ClientIPv4IPAddress, tsparams.ClientIPv6IPAddress})

				By("Validate IPv4 and IPv6 connectivity between the containers over the qinq tunnel.")
				err := cmd.ICMPConnectivityCheck(serverPod, []string{tsparams.ClientIPv4IPAddress,
					tsparams.ClientIPv6IPAddress}, "net2")
				Expect(err).ToNot(HaveOccurred(),
					"Failed to ping the client container over the 802.1ad connection")

				By("Validate IPv4 tcp traffic and dot1ad encapsulation from the client to server.")
				validateTCPTraffic(clientPod, serverIPV4IP.String())

				By("Validate IPv6 tcp traffic and dot1ad encapsulation from the client to server.")
				validateTCPTraffic(clientPod, serverIPV6IP.String())

				By("Validate that the TCP traffic is double tagged")
				readAndValidateTCPDump(tcpDumpContainer, tcpDumpReadFileCMD, tcpDumpDot1ADOutput)
			})

		It("Verify an 802.1ad QinQ tunneling between two containers with the VFs configured by NMState",
			reportxml.ID("71681"), func() {
				By("Define and create a container in promiscuous mode")
				//tcpDumpContainer := createPromiscuousClient(workerNodeList[0].Definition.Name,
				//	tcpDumpNet1CMD)

			})

		It("Verify network traffic over a 802.1ad Q-in-Q tunneling with Bond interfaces between two clients one SRIOV containers",
			reportxml.ID("71684"), func() {
				By("Define and create a container in promiscuous mode")
				//tcpDumpContainer := createPromiscuousClient(workerNodeList[0].Definition.Name,
				//	tcpDumpNet1CMD)

			})
	})

	Context("802.1Q", func() {
		BeforeAll(func() {
			By("Define and create sriov-network with 802.1q S-VLAN")
			vlan, err := strconv.Atoi(NetConfig.VLAN)
			Expect(err).ToNot(HaveOccurred(), "Failed to convert VLAN value")
			defineAndCreateSrIovNetworkWithQinQ(srIovNetworkDot1Q, srIovPolicyResNameNetDevice, dot1q,
				uint16(vlan))
		})

		It("Verify network traffic over a 802.1q QinQ tunnel between two SRIOV pods on the same PF",
			reportxml.ID("71677"), func() {
				By("Define and create a container in promiscuous mode")
				tcpDumpContainer := createPromiscuousClient(workerNodeList[0].Definition.Name,
					tcpDumpNet1CMD)

				By("Define and create a server container")
				serverPod := createServerTestPod(serverNameDot1q, srIovNetworkDot1Q, nadCVLAN100,
					workerNodeList[0].Definition.Name, []string{tsparams.ServerIPv4IPAddress, tsparams.ServerIPv6IPAddress})
				By("Define and create a client container")
				clientPod := createClientTestPod(clientNameDot1q, srIovNetworkDot1Q, nadCVLAN100,
					workerNodeList[0].Definition.Name, []string{tsparams.ClientIPv4IPAddress, tsparams.ClientIPv6IPAddress})

				By("Validate IPv4 and IPv6 connectivity between the containers over the qinq tunnel.")
				err := cmd.ICMPConnectivityCheck(serverPod, []string{tsparams.ClientIPv4IPAddress,
					tsparams.ClientIPv6IPAddress}, "net2")
				Expect(err).ToNot(HaveOccurred(),
					"Failed to ping the client container over the 802.1q connection")

				By("Validate IPv4 tcp traffic and dot1ad encapsulation from the client to server.")
				validateTCPTraffic(clientPod, serverIPV4IP.String())

				By("Validate IPv6 tcp traffic and dot1ad encapsulation from the client to server.")
				validateTCPTraffic(clientPod, serverIPV6IP.String())

				By("Validate that the TCP traffic is double tagged")
				readAndValidateTCPDump(tcpDumpContainer, tcpDumpReadFileCMD, tcpDumpDot1QOutput)
			})

		It("Verify network traffic over a 802.1Q QinQ tunnel between two SRIOV containers in different nodes",
			reportxml.ID("71679"), func() {
				By("Define and create a container in promiscuous mode")
				tcpDumpContainer := createPromiscuousClient(workerNodeList[0].Definition.Name,
					tcpDumpNet1CMD)

				By("Define and create a server container")
				serverPod := createServerTestPod(serverNameDot1q, srIovNetworkDot1Q, nadCVLAN100,
					workerNodeList[1].Definition.Name, []string{tsparams.ServerIPv4IPAddress, tsparams.ServerIPv6IPAddress})

				By("Define and create a client container")
				clientPod := createClientTestPod(clientNameDot1q, srIovNetworkDot1Q, nadCVLAN100,
					workerNodeList[0].Definition.Name, []string{tsparams.ClientIPv4IPAddress, tsparams.ClientIPv6IPAddress})

				By("Validate IPv4 and IPv6 connectivity between the containers over the qinq tunnel.")
				err := cmd.ICMPConnectivityCheck(serverPod, []string{tsparams.ClientIPv4IPAddress,
					tsparams.ClientIPv6IPAddress}, "net2")
				Expect(err).ToNot(HaveOccurred(),
					"Failed to ping the client container over the 802.1q connection.")

				By("Validate IPv4 tcp traffic and dot1ad encapsulation from the client to server.")
				validateTCPTraffic(clientPod, serverIPV4IP.String())

				By("Validate IPv6 tcp traffic and dot1ad encapsulation from the client to server.")
				validateTCPTraffic(clientPod, serverIPV6IP.String())

				By("Validate that the TCP traffic is double tagged")
				readAndValidateTCPDump(tcpDumpContainer, tcpDumpReadFileCMD, tcpDumpDot1QOutput)
			})
	})

	Context("dpdk", func() {
		BeforeAll(func() {
			//By("Deploying PerformanceProfile is it's not installed")
			//err = DeployPerformanceProfile(
			//	APIClient,
			//	NetConfig,
			//	"performance-profile-dpdk",
			//	"1,3,5,7,9,11,13,15,17,19,21,23,25",
			//	"0,2,4,6,8,10,12,14,16,18,20",
			//	24)
			//Expect(err).ToNot(HaveOccurred(), "Fail to deploy PerformanceProfile")

			By("Setting selinux flag container_use_devices to 1 on all compute nodes")
			err = cluster.ExecCmd(APIClient, NetConfig.WorkerLabel, "setsebool container_use_devices 1")
			Expect(err).ToNot(HaveOccurred(), "Fail to enable selinux flag")

			By("Define and create sriov-network with 802.1ad S-VLAN")
			vlan, err := strconv.Atoi(NetConfig.VLAN)
			Expect(err).ToNot(HaveOccurred(), "Failed to convert VLAN value")
			defineAndCreateSrIovNetworkWithQinQ(srIovNetworkDPDKDot1AD, srIovPolicyResNameVfioPci, dot1ad,
				uint16(vlan))

			By("Define and create sriov-network with 802.1q S-VLAN")
			vlan, err = strconv.Atoi(NetConfig.VLAN)
			Expect(err).ToNot(HaveOccurred(), "Failed to convert VLAN value")
			defineAndCreateSrIovNetworkWithQinQ(srIovNetworkDPDKDot1Q, srIovPolicyResNameVfioPci, dot1q,
				uint16(vlan))

			By("Define and create a network attachment definition for dpdk container")
			// sysConfig := map[string]string{"cniVersion": "0.4.0", "type": "tap", "selinuxcontext": "system_u:system_r:container_t:s0"}
			tapNad, err := define.TapNad(APIClient, nadCVLANDpdk, tsparams.TestNamespaceName, 0, 0, nil)
			Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Fail to define the Network-Attachment-Definition %s",
				nadCVLANDpdk))
			_, err = tapNad.Create()
			Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Fail to create Network-Attachment-Definition %s",
				nadCVLANDpdk))
		})

		FIt("Verify network traffic over a 802.1ad QinQ tunnel between two DPDK pods on the same PF",
			reportxml.ID("72636"), func() {
				By("Define and create a container in promiscuous mode")
				tcpDumpContainer := createPromiscuousClient(workerNodeList[0].Definition.Name,
					tcpDumpNet1CMD)

				By("Verify SR-IOV Device IDs for interface under test")
				if sriovDeviceID != intelDeviceIDE810 {
					Skip(fmt.Sprintf("The NIC %s does not support 802.1AD", sriovDeviceID))
				}

				By("Define and create a dpdk server container")

				annotation := pod.StaticIPAnnotationWithMacAddress(srIovNetworkDPDKDot1AD, []string{}, dpdkServerMac)
				testCmdServer := defineTestServerPmdCmd(dpdkClientMac, "${PCIDEVICE_OPENSHIFT_IO_SRIOVPOLICYVFIOPCI}")
				fmt.Println(testCmdServer)
				//[]string{"/bin/bash", "-c", "sleep INF"}
				_ = defineAndCreateServerDPDKPod(serverNameDPDKDot1ad, workerNodeList[0].Definition.Name, annotation, testCmdServer)

				By("Define and create a dpdk client container")
				var annotationDpdk []*types.NetworkSelectionElement
				sVlan := pod.StaticIPAnnotationWithMacAddress(srIovNetworkDPDKDot1AD, []string{}, dpdkClientMac)
				cVlan := pod.StaticAnnotation(nadCVLANDpdk)
				annotationDpdk = append(annotationDpdk, sVlan[0], cVlan)

				clientDpdk := defineAndCreateClientDPDKPod(clientNameDot1ad, workerNodeList[0].Definition.Name, annotationDpdk)

				testCmdClient := defineTestClientPmdCmd(dpdkClientMac, "${PCIDEVICE_OPENSHIFT_IO_SRIOVPOLICYVFIOPCI}")
				fmt.Println(testCmdClient)
				rxTrafficOnClientPod(clientDpdk, testCmdClient[0])
				// fmt.Sprintf(strings.Join(testCmdClient, ""))
				By("Validate that the TCP traffic is double tagged")
				readAndValidateTCPDump(tcpDumpContainer, tcpDumpReadFileCMD, tcpDumpDot1ADOutput)
			})

		It("Verify network traffic over a double tagging QinQ tunnel on the same PF between two DPDK clients",
			reportxml.ID("72638"), func() {
				By("Define and create a container in promiscuous mode")
				//tcpDumpContainer := createPromiscuousClient(workerNodeList[0].Definition.Name,
				//	tcpDumpNet1CMD)

			})
	})

	AfterEach(func() {
		By("Removing all containers from test namespace")
		runningNamespace, err := namespace.Pull(APIClient, tsparams.TestNamespaceName)
		Expect(err).ToNot(HaveOccurred(), "Failed to pull namespace")

		Expect(runningNamespace.CleanObjects(
			tsparams.WaitTimeout, pod.GetGVR())).ToNot(HaveOccurred(), "Failed to the test namespace")
	})

	AfterAll(func() {
		By("Remove the double tag switch interface configurations")

		err = disableQinQOnSwitch(switchCredentials, switchInterfaces)
		Expect(err).ToNot(HaveOccurred(),
			"Failed to remove VLAN double tagging configuration from the switch")

		By(fmt.Sprintf("Disable VF promiscuous support on %s", srIovInterfacesUnderTest[0]))
		if sriovDeviceID == mlxDevice {
			promiscVFCommand = fmt.Sprintf("ip link set %s promisc off",
				srIovInterfacesUnderTest[0])
		} else {
			promiscVFCommand = fmt.Sprintf("ethtool --set-priv-flags %s vf-true-promisc-support off",
				srIovInterfacesUnderTest[0])
		}

		By(fmt.Sprintf("Disable VF promiscuous support on %s", srIovInterfacesUnderTest[0]))
		output, err := cmd.RunCommandOnHostNetworkPod(workerNodeList[0].Definition.Name, NetConfig.SriovOperatorNamespace,
			promiscVFCommand)
		Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Failed to run command on node %s", output))

		By("Removing all SR-IOV Policy")
		err = sriov.CleanAllNetworkNodePolicies(APIClient, NetConfig.SriovOperatorNamespace, metav1.ListOptions{})
		Expect(err).ToNot(HaveOccurred(), "Failed to clean srIovPolicy")

		By("Removing all srIovNetworks")
		err = sriov.CleanAllNetworksByTargetNamespace(
			APIClient, NetConfig.SriovOperatorNamespace, tsparams.TestNamespaceName, metav1.ListOptions{})
		Expect(err).ToNot(HaveOccurred(), "Failed to clean sriov networks")

		By("Waiting until cluster MCP and SR-IOV are stable")
		err = netenv.WaitForSriovAndMCPStable(
			APIClient, tsparams.MCOWaitTimeout, time.Minute, NetConfig.CnfMcpLabel, NetConfig.SriovOperatorNamespace)
		Expect(err).ToNot(HaveOccurred(), "Failed cluster is not stable")
	})
})

func defineAndCreateSrIovNetworkWithQinQ(srIovNetwork, resName, vlanProtocol string, vlan uint16) {
	srIovNetworkObject, err := sriov.NewNetworkBuilder(
		APIClient, srIovNetwork, NetConfig.SriovOperatorNamespace, tsparams.TestNamespaceName, resName).
		WithVlanProto(vlanProtocol).WithVLAN(vlan).Create()
	Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Failed to create sriov network %s", err))

	Eventually(func() bool {
		_, err := nad.Pull(APIClient, srIovNetworkObject.Object.Name, tsparams.TestNamespaceName)

		return err == nil
	}, tsparams.WaitTimeout, tsparams.RetryInterval).Should(BeTrue(), "Fail to pull NetworkAttachmentDefinition")
}

func createPromiscuousClient(nodeName string, tcpDumpCMD []string) *pod.Builder {
	sriovNetworkDefault := pod.StaticIPAnnotation("sriovnetwork-promiscuous", []string{"192.168.100.1/24"})

	clientDefault, err := pod.NewBuilder(APIClient, "client-promiscuous", tsparams.TestNamespaceName,
		NetConfig.CnfNetTestContainer).DefineOnNode(nodeName).WithPrivilegedFlag().RedefineDefaultCMD(tcpDumpCMD).
		WithSecondaryNetwork(sriovNetworkDefault).CreateAndWaitUntilRunning(netparam.DefaultTimeout)
	Expect(err).ToNot(HaveOccurred(), "Failed to define and run promiscuous pod")

	return clientDefault
}

func createServerTestPod(name, sVlan, cVlan, nodeName string, ipAddress []string) *pod.Builder {
	By(fmt.Sprintf("Define and run test pod  %s", name))

	annotation := defineNetworkAnnotation(sVlan, cVlan, ipAddress)

	serverCmd := []string{"bash", "-c", "sleep 5; testcmd -interface net2 -protocol tcp -port 4444 -listen"}
	serverBuild, err := pod.NewBuilder(APIClient, name, tsparams.TestNamespaceName,
		NetConfig.CnfNetTestContainer).DefineOnNode(nodeName).WithSecondaryNetwork(annotation).
		RedefineDefaultCMD(serverCmd).WithPrivilegedFlag().CreateAndWaitUntilRunning(netparam.DefaultTimeout)
	Expect(err).ToNot(HaveOccurred(), "Failed to define and run server container")

	return serverBuild
}

func createDPDKServerTestPod(name, sVlan, cVlan, nodeName string, ipAddress []string) *pod.Builder {
	By(fmt.Sprintf("Define and run test pod  %s", name))

	annotation := defineNetworkAnnotation(sVlan, cVlan, ipAddress)

	serverCmd := []string{"bash", "-c", "sleep 5; testcmd -interface net2 -protocol tcp -port 4444 -listen"}
	serverBuild, err := pod.NewBuilder(APIClient, name, tsparams.TestNamespaceName,
		NetConfig.DpdkTestContainer).DefineOnNode(nodeName).WithSecondaryNetwork(annotation).
		RedefineDefaultCMD(serverCmd).WithPrivilegedFlag().CreateAndWaitUntilRunning(netparam.DefaultTimeout)
	Expect(err).ToNot(HaveOccurred(), "Failed to define and run server container")

	return serverBuild
}

func defineAndCreateServerDPDKPod(
	podName,
	nodeName string,
	serverPodNetConfig []*types.NetworkSelectionElement,
	podCmd []string) *pod.Builder {
	var rootUser int64 = 0
	securityContext := corev1.SecurityContext{
		RunAsUser: &rootUser,
		Capabilities: &corev1.Capabilities{
			Add: []corev1.Capability{"IPC_LOCK", "SYS_RESOURCE", "NET_RAW"},
		},
	}

	dpdkContainer := pod.NewContainerBuilder(podName, NetConfig.DpdkTestContainer, podCmd)
	dpdkContainerCfg, err := dpdkContainer.WithSecurityContext(&securityContext).
		WithResourceLimit("2Gi", "1Gi", 4).
		WithResourceRequest("2Gi", "1Gi", 4).WithEnvVar("RUN_TYPE", "testcmd").
		GetContainerCfg()

	Expect(err).ToNot(HaveOccurred(), "Fail to set dpdk container")

	dpdkPod := pod.NewBuilder(APIClient, podName, tsparams.TestNamespaceName,
		NetConfig.DpdkTestContainer).WithSecondaryNetwork(serverPodNetConfig).
		DefineOnNode(nodeName).RedefineDefaultContainer(*dpdkContainerCfg).WithHugePages()
	configMapData := map[string]string{"cmd_file": tsparams.DpdkPort0Cmd}
	configMap, err := configmap.NewBuilder(APIClient, "dpdk-port-cmd", tsparams.TestNamespaceName).
		WithData(configMapData).Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create config map")
	dpdkPod.WithLocalVolume(configMap.Definition.Name, "/etc/cmd")
	dpdkPod, err = dpdkPod.CreateAndWaitUntilRunning(10 * time.Minute)
	Expect(err).ToNot(HaveOccurred(), "Fail to create server pod")

	return dpdkPod
}

func defineAndCreateClientDPDKPod(
	podName,
	nodeName string,
	serverPodNetConfig []*types.NetworkSelectionElement) *pod.Builder {
	var rootUser int64 = 0
	securityContext := corev1.SecurityContext{
		RunAsUser: &rootUser,
		Capabilities: &corev1.Capabilities{
			Add: []corev1.Capability{"IPC_LOCK", "SYS_RESOURCE", "NET_RAW"},
		},
	}

	dpdkContainer := pod.NewContainerBuilder(podName, NetConfig.DpdkTestContainer, []string{"/bin/bash", "-c", "sleep INF"})
	dpdkContainerCfg, err := dpdkContainer.WithSecurityContext(&securityContext).
		WithResourceLimit("2Gi", "1Gi", 4).
		WithResourceRequest("2Gi", "1Gi", 4).WithEnvVar("RUN_TYPE", "testcmd").
		GetContainerCfg()
	Expect(err).ToNot(HaveOccurred(), "Fail to set dpdk container")

	dpdkPod := pod.NewBuilder(APIClient, podName, tsparams.TestNamespaceName,
		NetConfig.DpdkTestContainer).WithSecondaryNetwork(serverPodNetConfig).
		DefineOnNode(nodeName).RedefineDefaultContainer(*dpdkContainerCfg).WithHugePages()

	dpdkPod, err = dpdkPod.CreateAndWaitUntilRunning(10 * time.Minute)
	Expect(err).ToNot(HaveOccurred(), "Fail to create server pod")

	return dpdkPod
}

func createClientTestPod(name, sVlan, cVlan, nodeName string, ipAddress []string) *pod.Builder {
	By(fmt.Sprintf("Define and run test pod  %s", name))

	annotation := defineNetworkAnnotation(sVlan, cVlan, ipAddress)

	clientBuild, err := pod.NewBuilder(APIClient, name, tsparams.TestNamespaceName,
		NetConfig.CnfNetTestContainer).DefineOnNode(nodeName).WithSecondaryNetwork(annotation).
		WithPrivilegedFlag().CreateAndWaitUntilRunning(netparam.DefaultTimeout)
	Expect(err).ToNot(HaveOccurred(), "Failed to define and run default client")

	return clientBuild
}

func defineNetworkAnnotation(sVlan, cVlan string, ipAddress []string) []*multus.NetworkSelectionElement {
	annotation := []*multus.NetworkSelectionElement{}
	svlanAnnotation := pod.StaticAnnotation(sVlan)
	cvlanAnnotation := pod.StaticIPAnnotation(cVlan, ipAddress)
	annotation = append(annotation, svlanAnnotation, cvlanAnnotation[0])

	return annotation
}

func defineNetworkAnnotationServer(sVlan, cVlan string, cVlan2 ...string) []*multus.NetworkSelectionElement {
	annotation := []*multus.NetworkSelectionElement{}
	svlanAnnotation := pod.StaticAnnotation(sVlan)
	cvlanAnnotation := pod.StaticIPAnnotation(cVlan, []string{tsparams.ServerIPv4IPAddress,
		tsparams.ServerIPv6IPAddress})

	if len(cVlan2) != 0 {
		cvlanAnnotation2 := pod.StaticIPAnnotation(cVlan2[0], []string{tsparams.ServerIPv4IPAddress2,
			tsparams.ServerIPv6IPAddress2})

		return append(annotation, svlanAnnotation, cvlanAnnotation[0], cvlanAnnotation2[0])
	}
	annotation = append(annotation, svlanAnnotation, cvlanAnnotation[0])

	return annotation
}

func defineNetworkAnnotationClient(sVlan, cVlan string, cVlan2 ...string) []*multus.NetworkSelectionElement {
	annotation := []*multus.NetworkSelectionElement{}
	svlanAnnotation := pod.StaticAnnotation(sVlan)
	cvlanAnnotation := pod.StaticIPAnnotation(cVlan, []string{tsparams.ClientIPv4IPAddress,
		tsparams.ClientIPv6IPAddress})

	if len(cVlan2) != 0 {
		cvlanAnnotation2 := pod.StaticIPAnnotation(cVlan2[0], []string{tsparams.ClientIPv4IPAddress2,
			tsparams.ClientIPv6IPAddress2})

		return append(annotation, svlanAnnotation, cvlanAnnotation[0], cvlanAnnotation2[0])
	}
	annotation = append(annotation, svlanAnnotation, cvlanAnnotation[0])

	return annotation
}

func discoverInterfaceUnderTestDeviceID(srIovInterfaceUnderTest, workerNodeName string) string {
	sriovInterfaces, err := sriov.NewNetworkNodeStateBuilder(
		APIClient, workerNodeName, NetConfig.SriovOperatorNamespace).GetUpNICs()
	Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("fail to discover device ID for network interface %s",
		srIovInterfaceUnderTest))

	for _, srIovInterface := range sriovInterfaces {
		if srIovInterface.Name == srIovInterfaceUnderTest {
			return srIovInterface.DeviceID
		}
	}

	return ""
}

func validateTCPTraffic(clientPod *pod.Builder, destIPAddr string) {
	command := []string{
		"testcmd",
		fmt.Sprintf("--interface=%s", "net2"),
		fmt.Sprintf("--server=%s", destIPAddr),
		"--protocol=tcp",
		"--mtu=100",
		"--port=4444",
	}

	outPut, err := clientPod.ExecCommand(
		command)
	Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Fail to run testcmd on %s command output: %s",
		clientPod.Definition.Name, outPut.String()))
}

// readAndValidateTCPDump checks that the inner C-VLAN is present verifying that the packet was double tagged.
func readAndValidateTCPDump(clientPod *pod.Builder, testCmd []string, pattern string) {
	By("Start to capture traffic on the promiscuous client")

	output, err := clientPod.ExecCommand(testCmd)
	Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Error executing command: %s", output.String()))

	err = validateDot1Encapsulation(output.String(), pattern)
	Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Failed to validate qinq encapsulation %s", output.String()))
}

func validateDot1Encapsulation(fileOutput, dot1X string) error {
	// Compile the regular expression
	regex := regexp.MustCompile(dot1X)
	fmt.Println("REGEX", regex.String())
	match := regex.FindStringSubmatch(fileOutput)

	// Check if the regular expression matched at all
	if len(match) == 0 {
		return fmt.Errorf("regular expression did not match")
	}

	if len(match) != 3 {
		return fmt.Errorf("failed to match double encapsulation")
	}
	// Output the matches
	fmt.Println("Matched S-VLAN", match[1])
	fmt.Println("Matched C-VLAN", match[2])

	return nil
}

func enableDot1ADonSwitchInterfaces(credentials *sriovenv.SwitchCredentials, switchInterfaces []string) error {
	jnpr, err := cmd.NewSession(credentials.SwitchIP, credentials.User, credentials.Password)
	if err != nil {
		return err
	}
	defer jnpr.Close()

	for _, switchInterface := range switchInterfaces {
		commands := []string{fmt.Sprintf("set interfaces %s vlan-tagging encapsulation extended-vlan-bridge",
			switchInterface)}

		err = jnpr.Config(commands)
		if err != nil {
			return err
		}
	}

	return nil
}

func disableQinQOnSwitch(switchCredentials *sriovenv.SwitchCredentials, switchInterfaces []string) error {
	jnpr, err := cmd.NewSession(switchCredentials.SwitchIP, switchCredentials.User, switchCredentials.Password)
	if err != nil {
		return err
	}
	defer jnpr.Close()

	for _, switchInterface := range switchInterfaces {
		commands := []string{fmt.Sprintf("delete interfaces %s vlan-tagging", switchInterface)}
		commands = append(commands, fmt.Sprintf("delete interfaces %s encapsulation extended-vlan-bridge",
			switchInterface))
		err = jnpr.Config(commands)

		if err != nil {
			return err
		}
	}

	return nil
}

func defineTestServerPmdCmd(ethPeer, pciAddress string) []string {
	baseCmd := fmt.Sprintf("dpdk-testpmd -a %s -- --forward-mode txonly --eth-peer=0,%s --cmdline-file=/etc/cmd/cmd_file", pciAddress, ethPeer)

	baseCmd += " --stats-period 5"

	return []string{"/bin/bash", "-c", baseCmd}
}

func defineTestClientPmdCmd(ethPeer, pciAddress string) []string {
	baseCmd := fmt.Sprintf("timeout -s SIGKILL 20 dpdk-testpmd --vdev=virtio_user0,path=/dev/vhost-net,iface=net2 -a %s -- --forward-mode txonly --eth-peer=0,%s", pciAddress, ethPeer)

	baseCmd += " --stats-period 5"

	return []string{baseCmd}
}

func rxTrafficOnClientPod(clientPod *pod.Builder, clientRxCmd string) {
	Expect(clientPod.WaitUntilRunning(time.Minute)).ToNot(HaveOccurred(), "Fail to wait until pod is running")
	clientOut, err := clientPod.ExecCommand([]string{"/bin/bash", "-c", clientRxCmd})
	var timeoutError = "command terminated with exit code 137"
	if err.Error() != timeoutError {
		Expect(err).ToNot(HaveOccurred(), "Fail to exec cmd")
	}

	By("Parsing output from the DPDK application")
	glog.V(90).Infof("Processing testpdm output from client pod \n%s", clientOut.String())
	Expect(checkRxOnly(clientOut.String())).Should(BeTrue(), "Fail to process output from dpdk application")
}

func checkRxOnly(out string) bool {
	lines := strings.Split(out, "\n")
	Expect(len(lines)).To(BeNumerically(">=", 3),
		"Fail line list contains less than 3 elements")

	for i, line := range lines {
		if strings.Contains(line, "NIC statistics for port") {
			if len(lines) > i && getNumberOfPackets(lines[i+1], "RX") > 0 {
				return true
			}
		}
	}

	return false
}

func getNumberOfPackets(line, firstFieldSubstr string) int {
	splitLine := strings.Fields(line)
	Expect(splitLine[0]).To(ContainSubstring(firstFieldSubstr), "Fail to find expected substring")
	Expect(len(splitLine)).To(Equal(6), "the slice doesn't contain 6 elements")
	numberOfPackets, err := strconv.Atoi(splitLine[1])
	Expect(err).ToNot(HaveOccurred(), "Fail to convert string to integer")

	return numberOfPackets
}

// DeployPerformanceProfile installs performanceProfile on cluster.
func DeployPerformanceProfile(
	apiClient *clients.Settings,
	netConfig *netconfig.NetworkConfig,
	profileName string,
	isolatedCPU string,
	reservedCPU string,
	hugePages1GCount int32) error {
	glog.V(90).Infof("Ensuring cluster has correct PerformanceProfile deployed")

	mcp, err := mco.Pull(apiClient, netConfig.CnfMcpLabel)
	if err != nil {
		return fmt.Errorf("fail to pull MCP due to : %w", err)
	}

	performanceProfiles, err := nto.ListProfiles(apiClient)

	if err != nil {
		return fmt.Errorf("fail to list PerformanceProfile objects on cluster due to: %w", err)
	}

	if len(performanceProfiles) > 0 {
		for _, perfProfile := range performanceProfiles {
			if perfProfile.Object.Name == profileName {
				glog.V(90).Infof("PerformanceProfile %s exists", profileName)

				return nil
			}
		}

		glog.V(90).Infof("PerformanceProfile doesn't exist on cluster. Removing all pre-existing profiles")

		err := nto.CleanAllPerformanceProfiles(apiClient)

		if err != nil {
			return fmt.Errorf("fail to clean pre-existing performance profiles due to %w", err)
		}

		err = mcp.WaitToBeStableFor(time.Minute, tsparams.MCOWaitTimeout)

		if err != nil {
			return err
		}
	}

	glog.V(90).Infof("Required PerformanceProfile doesn't exist. Installing new profile PerformanceProfile")

	_, err = nto.NewBuilder(apiClient, profileName, isolatedCPU, reservedCPU, netConfig.WorkerLabelMap).
		WithHugePages("1G", []v2.HugePage{{Size: "1G", Count: hugePages1GCount}}).Create()

	if err != nil {
		return fmt.Errorf("fail to deploy PerformanceProfile due to: %w", err)
	}

	return mcp.WaitToBeStableFor(time.Minute, tsparams.MCOWaitTimeout)
}
