package tests

import (
	"fmt"
	"github.com/openshift-kni/eco-gotests/vendor/github.com/openshift-kni/eco-goinfra/pkg/metallb"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/deployment"
	"github.com/openshift-kni/eco-goinfra/pkg/nmstate"
	"github.com/openshift-kni/eco-goinfra/pkg/nodes"
	"github.com/openshift-kni/eco-goinfra/pkg/pod"
	"github.com/openshift-kni/eco-goinfra/pkg/reportxml"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netnmstate"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netparam"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/frr"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/metallbenv"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/tsparams"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var _ = Describe("MetalLB NodeSelector", Ordered, Label(tsparams.LabelBGPTestCases), ContinueOnFailure, func() {
	var (
		externalAdvertisedIPv4Routes = []string{"192.168.100.0/24", "192.168.200.0/24"}
		externalAdvertisedIPv6Routes = []string{"2001:100::0/64", "2001:200::0/64"}
		hubIPv4ExternalAddresses     = []string{"172.16.0.10", "172.16.0.11"}
		frrExternalMasterIPAddress   = "172.16.0.1"
		frrNodeSecIntIPv4Addresses   = []string{"10.100.100.254", "10.100.100.253"}
		hubPodWorker0                = "hub-pod-worker-0"
		hubPodWorker1                = "hub-pod-worker-1"
		frrK8WebHookServer           = "frr-k8s-webhook-server"
		frrCongigAllowAll            = "frrconfig-allow-all"
		frrNodeLabel                 = "app=frr-k8s"
		err                          error
	)

	BeforeAll(func() {

		By("Getting MetalLb load balancer ip addresses")
		ipv4metalLbIPList, ipv6metalLbIPList, err = metallbenv.GetMetalLbIPByIPStack()
		Expect(err).ToNot(HaveOccurred(), tsparams.MlbAddressListError)

		By("List CNF worker nodes in cluster")
		cnfWorkerNodeList, err = nodes.List(APIClient,
			metav1.ListOptions{LabelSelector: labels.Set(NetConfig.WorkerLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Failed to discover worker nodes")

		By("Selecting worker node for BGP tests")
		workerLabelMap, workerNodeList = setWorkerNodeListAndLabelForBfdTests(cnfWorkerNodeList, metalLbTestsLabel)
		ipv4NodeAddrList, err = nodes.ListExternalIPv4Networks(
			APIClient, metav1.ListOptions{LabelSelector: labels.Set(workerLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Failed to collect external nodes ip addresses")

		err = metallbenv.IsEnvVarMetalLbIPinNodeExtNetRange(ipv4NodeAddrList, ipv4metalLbIPList, nil)
		Expect(err).ToNot(HaveOccurred(), "Failed to validate metalLb exported ip address")

		By("Listing master nodes")
		masterNodeList, err = nodes.List(APIClient,
			metav1.ListOptions{LabelSelector: labels.Set(NetConfig.ControlPlaneLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Fail to list master nodes")
		Expect(len(masterNodeList)).To(BeNumerically(">", 0),
			"Failed to detect master nodes")
	})

	AfterAll(func() {
		if len(cnfWorkerNodeList) > 2 {
			By("Remove custom metallb test label from nodes")
			removeNodeLabel(workerNodeList, metalLbTestsLabel)
		}
	})

	Context("Single IPAddressPool", func() {

		var (
			nodeAddrList []string
			addressPool  []string
			frrk8sPods   []*pod.Builder
			err          error
		)

		BeforeAll(func() {
			By("Setting test iteration parameters")
			_, _, _, nodeAddrList, addressPool, _, err =
				metallbenv.DefineIterationParams(
					ipv4metalLbIPList, ipv6metalLbIPList, ipv4NodeAddrList, ipv6NodeAddrList, netparam.IPV4Family)
			Expect(err).ToNot(HaveOccurred(), "Fail to set iteration parameters")

			By("Creating a new instance of MetalLB Speakers on workers")
			err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
			Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

			By("Waiting until the new frr-k8s-webhook-server deployment is in Ready state.")
			frrk8sWebhookDeployment, err := deployment.Pull(
				APIClient, frrK8WebHookServer, NetConfig.MlbOperatorNamespace)
			Expect(err).ToNot(HaveOccurred(), "Fail to pull frr-k8s-webhook-server")
			Expect(frrk8sWebhookDeployment.IsReady(30*time.Second)).To(BeTrue(),
				"frr-k8s-webhook-server deployment is not ready")
		})

		AfterEach(func() {
			By("Clean metallb operator and test namespaces")
			resetOperatorAndTestNS()
		})

		It("Advertise a single IPAddressPool with different attributes using the node selector option",
			reportxml.ID("53987"), func() {
				prefixToFilter := externalAdvertisedIPv4Routes[1]

				By("Create two IPAddressPools")
				ipAddressPoool1 := createIPAddressPool("ipaddresspool1", "192.168.100.0/24")

				By("Creating a MetalLB service")
				setupMetalLbService(netparam.IPV4Family, ipAddressPoool1, "Cluster")

				By("Creating static ip annotation")
				staticIPAnnotation := pod.StaticIPAnnotation(
					externalNad.Definition.Name, []string{fmt.Sprintf("%s/%s", ipv4metalLbIPList[0], "24")})

				By("Creating MetalLb configMap")
				masterConfigMap := createConfigMap(tsparams.LocalBGPASN, ipv4NodeAddrList, false, false)

				By("Creating FRR Pod")
				frrPod := createFrrPod(
					masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, staticIPAnnotation)

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady(ipv4metalLbIPList[0], "", tsparams.LocalBGPASN,
					false, 0, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))

				By("Validating BGP route prefix")
				validatePrefix(frrPod, netparam.IPV4Family, removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create a frrconfiguration with prefix filter")

				createFrrConfiguration("frrconfig-filtered", ipv4metalLbIPList[0],
					tsparams.LocalBGPASN, []string{externalAdvertisedIPv4Routes[0], externalAdvertisedIPv6Routes[0]},
					false, false)

				frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
					LabelSelector: frrNodeLabel,
				})
				Expect(err).ToNot(HaveOccurred(), "Fail to find Frrk8 pod list")

				By("Verify that the node FRR pods advertises two routes")
				verifyExternalAdvertisedRoutes(frrPod, ipv4NodeAddrList, externalAdvertisedIPv4Routes)

				By("Validate BGP received routes")
				verifyReceivedRoutes(frrk8sPods, externalAdvertisedIPv4Routes[0])
				By("Validate BGP route is filtered")
				verifyBlockedRoutes(frrk8sPods, prefixToFilter)
			})
	})

	Context("Dual IPAddressPools", func() {

		var (
			frrk8sPods        []*pod.Builder
			masterClientPodIP string
			nodeAddrList      []string
			addressPool       []string
		)

		BeforeEach(func() {
			By("Creating a new instance of MetalLB Speakers on workers")
			err := metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
			Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

			By("Waiting until the new frr-k8s-webhook-server deployment is in Ready state.")
			frrk8sWebhookDeployment, err := deployment.Pull(
				APIClient, frrK8WebHookServer, NetConfig.MlbOperatorNamespace)
			Expect(err).ToNot(HaveOccurred(), "Fail to pull frr-k8s-webhook-server")
			Expect(frrk8sWebhookDeployment.IsReady(30*time.Second)).To(BeTrue(),
				"frr-k8s-webhook-server deployment is not ready")

			By("Collecting information before test")
			frrk8sPods, err = pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
				LabelSelector: frrNodeLabel,
			})
			Expect(err).ToNot(HaveOccurred(), "Failed to list speaker pods")
			By("Setting test iteration parameters")
			masterClientPodIP, _, _, nodeAddrList, addressPool, _, err =
				metallbenv.DefineIterationParams(
					ipv4metalLbIPList, ipv6metalLbIPList, ipv4NodeAddrList, ipv6NodeAddrList, netparam.IPV4Family)
			Expect(err).ToNot(HaveOccurred(), "Fail to set iteration parameters")

			By("Creating an IPAddressPool and BGPAdvertisement")
			ipAddressPool := setupBgpAdvertisement(addressPool, int32(32))

			By("Creating a MetalLB service")
			setupMetalLbService(netparam.IPV4Family, ipAddressPool, "Cluster")

			By("Creating nginx test pod on worker node")
			setupNGNXPod(workerNodeList[0].Definition.Name)
		})

		AfterAll(func() {
			By("Clean metallb operator and test namespaces")
			resetOperatorAndTestNS()
		})

		AfterEach(func() {
			By("Removing static routes from the speakers")
			frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
				LabelSelector: tsparams.FRRK8sDefaultLabel,
			})
			Expect(err).ToNot(HaveOccurred(), "Failed to list pods")

			speakerRoutesMap := buildRoutesMapWithSpecificRoutes(frrk8sPods, []string{ipv4metalLbIPList[0],
				ipv4metalLbIPList[1], frrNodeSecIntIPv4Addresses[0], frrNodeSecIntIPv4Addresses[1]})

			for _, frrk8sPod := range frrk8sPods {
				out, err := frr.SetStaticRoute(frrk8sPod, "del", frrExternalMasterIPAddress, speakerRoutesMap)
				Expect(err).ToNot(HaveOccurred(), out)
			}

			srIovInterfacesUnderTest, err := NetConfig.GetSriovInterfaces(1)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve SR-IOV interfaces for testing")

			vlanID, err := NetConfig.GetVLAN()
			Expect(err).ToNot(HaveOccurred(), "Fail to set vlanID")

			By("Removing secondary interface on worker node 0")
			secIntWorker0Policy := nmstate.NewPolicyBuilder(APIClient, "sec-int-worker0", NetConfig.WorkerLabelMap).
				WithAbsentInterface(fmt.Sprintf("%s.%d", srIovInterfacesUnderTest[0], vlanID))
			err = netnmstate.UpdatePolicyAndWaitUntilItsAvailable(netparam.DefaultTimeout, secIntWorker0Policy)
			Expect(err).ToNot(HaveOccurred(), "Failed to update NMState network policy")

			By("Removing secondary interface on worker node 1")
			secIntWorker1Policy := nmstate.NewPolicyBuilder(APIClient, "sec-int-worker1", NetConfig.WorkerLabelMap).
				WithAbsentInterface(fmt.Sprintf("%s.%d", srIovInterfacesUnderTest[0], vlanID))
			err = netnmstate.UpdatePolicyAndWaitUntilItsAvailable(netparam.DefaultTimeout, secIntWorker1Policy)
			Expect(err).ToNot(HaveOccurred(), "Failed to update NMState network policy")

			By("Collect list of nodeNetworkConfigPolicies and delete them.")
			By("Removing NMState policies")
			err = nmstate.CleanAllNMStatePolicies(APIClient)
			Expect(err).ToNot(HaveOccurred(), "Failed to remove all NMState policies")

			By("Reset metallb operator namespaces")
			resetOperatorAndTestNS()
		})

		It("Validate a FRR node receives and sends IPv4 and IPv6 routes from an IBGP multihop FRR instance",
			reportxml.ID("74278"), func() {

				By("Adding static routes to the speakers")
				speakerRoutesMap := buildRoutesMapWithSpecificRoutes(frrk8sPods, ipv4metalLbIPList)

				for _, frrk8sPod := range frrk8sPods {
					out, err := frr.SetStaticRoute(frrk8sPod, "add", masterClientPodIP, speakerRoutesMap)
					Expect(err).ToNot(HaveOccurred(), out)
				}

				By("Creating External NAD for master FRR pod")
				createExternalNad(tsparams.ExternalMacVlanNADName)

				By("Creating External NAD for hub FRR pods")
				createExternalNad(tsparams.HubMacVlanNADName)

				By("Creating static ip annotation for hub0")
				hub0BRstaticIPAnnotation := createStaticIPAnnotations(tsparams.ExternalMacVlanNADName,
					tsparams.HubMacVlanNADName,
					[]string{fmt.Sprintf("%s/24", ipv4metalLbIPList[0])},
					[]string{fmt.Sprintf("%s/24", hubIPv4ExternalAddresses[0])})

				By("Creating static ip annotation for hub1")
				hub1BRstaticIPAnnotation := createStaticIPAnnotations(tsparams.ExternalMacVlanNADName,
					tsparams.HubMacVlanNADName,
					[]string{fmt.Sprintf("%s/24", ipv4metalLbIPList[1])},
					[]string{fmt.Sprintf("%s/24", hubIPv4ExternalAddresses[1])})

				By("Creating MetalLb Hub pod configMap")
				hubConfigMap := createHubConfigMap("frr-hub-node-config")

				By("Creating FRR Hub pod on worker node 0")
				_ = createFrrHubPod(hubPodWorker0,
					workerNodeList[0].Object.Name, hubConfigMap.Definition.Name, []string{}, hub0BRstaticIPAnnotation)

				By("Creating FRR Hub pod on worker node 1")
				_ = createFrrHubPod(hubPodWorker1,
					workerNodeList[1].Object.Name, hubConfigMap.Definition.Name, []string{}, hub1BRstaticIPAnnotation)

				By("Creating configmap and MetalLb Master pod")
				frrPod := createMasterFrrPod(tsparams.LocalBGPASN, frrExternalMasterIPAddress, nodeAddrList,
					hubIPv4ExternalAddresses, externalAdvertisedIPv4Routes,
					externalAdvertisedIPv6Routes, false)

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady(frrExternalMasterIPAddress, "", tsparams.LocalBGPASN,
					false, 0, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))

				By("Validating BGP route prefix")
				validatePrefix(frrPod, netparam.IPV4Family, removePrefixFromIPList(nodeAddrList),
					addressPool, 32)

				By("Create a frrconfiguration allow all for EBGP multihop")
				createFrrConfiguration(frrCongigAllowAll, frrExternalMasterIPAddress,
					tsparams.LocalBGPASN, nil,
					false, false)

				By("Verify that the node FRR pods advertises two routes")
				verifyExternalAdvertisedRoutes(frrPod, ipv4NodeAddrList, externalAdvertisedIPv4Routes)

				By("Validate that both BGP routes are received")
				verifyReceivedRoutes(frrk8sPods, externalAdvertisedIPv4Routes[0])
				verifyReceivedRoutes(frrk8sPods, externalAdvertisedIPv4Routes[1])
			})
	})
})

func createIPAddressPool(name, ipPrefix string) *metallb.IPAddressPoolBuilder {
	ipAddressPool, err := metallb.NewIPAddressPoolBuilder(
		APIClient,
		name,
		NetConfig.MlbOperatorNamespace,
		[]string{ipPrefix}).Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create IPAddressPool")

	return ipAddressPool
}
