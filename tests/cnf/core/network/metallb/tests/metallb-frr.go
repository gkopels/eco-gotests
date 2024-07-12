package tests

import (
	"fmt"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/configmap"
	"github.com/openshift-kni/eco-goinfra/pkg/metallb"
	"github.com/openshift-kni/eco-goinfra/pkg/nad"
	"github.com/openshift-kni/eco-goinfra/pkg/namespace"
	"github.com/openshift-kni/eco-goinfra/pkg/nodes"
	"github.com/openshift-kni/eco-goinfra/pkg/pod"
	"github.com/openshift-kni/eco-goinfra/pkg/reportxml"
	"github.com/openshift-kni/eco-goinfra/pkg/service"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/frr"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/metallbenv"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/tsparams"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"net"
	"strings"
	"time"
)

var _ = Describe("FRR", Ordered, Label(tsparams.LabelFRRTestCases), ContinueOnFailure, func() {

	BeforeAll(func() {
		var err error
		By("Getting MetalLb load balancer ip addresses")
		ipv4metalLbIPList, ipv6metalLbIPList, err = metallbenv.GetMetalLbIPByIPStack()
		fmt.Println("ipv4metalLbIPList", ipv4metalLbIPList)
		Expect(err).ToNot(HaveOccurred(), "An unexpected error occurred while "+
			"determining the IP addresses from the ECO_CNF_CORE_NET_MLB_ADDR_LIST environment variable.")

		By("Getting external nodes ip addresses")
		cnfWorkerNodeList, err = nodes.List(APIClient,
			metav1.ListOptions{LabelSelector: labels.Set(NetConfig.WorkerLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Failed to discover worker nodes")

		By("Selecting worker node for BGP tests")
		workerLabelMap, workerNodeList = setWorkerNodeListAndLabelForBfdTests(cnfWorkerNodeList, metalLbTestsLabel)
		ipv4NodeAddrList, err = nodes.ListExternalIPv4Networks(
			APIClient, metav1.ListOptions{LabelSelector: labels.Set(workerLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Failed to collect external nodes ip addresses")

		By("Creating a new instance of MetalLB Speakers on workers")
		err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunningWithAlwaysBlock(tsparams.DefaultTimeout, workerLabelMap,
			[]string{"192.168.100.0/24"})
		Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

		err = metallbenv.IsEnvVarMetalLbIPinNodeExtNetRange(ipv4NodeAddrList, ipv4metalLbIPList, nil)
		Expect(err).ToNot(HaveOccurred(), "Failed to validate metalLb exported ip address")

		By("Listing master nodes")
		masterNodeList, err = nodes.List(APIClient,
			metav1.ListOptions{LabelSelector: labels.Set(NetConfig.ControlPlaneLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Fail to list master nodes")
		Expect(len(masterNodeList)).To(BeNumerically(">", 0),
			"Failed to detect master nodes")
	})

	//var speakerPods []*pod.Builder
	var frrPods []*pod.Builder

	BeforeEach(func() {
		By("Creating a new instance of MetalLB Speakers on workers")
		err := metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
		Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

		By("Creating External NAD")
		createExternalNad()

		By("Listing metalLb speaker and frr pods")
		speakerPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
			LabelSelector: tsparams.MetalLbDefaultSpeakerLabel,
		})
		Expect(err).ToNot(HaveOccurred(), "Fail to list speaker pods")
		Expect(len(speakerPods)).To(BeNumerically(">", 0),
			"Failed the number of frr speaker pods is 0")

		frrPods, err = pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
			LabelSelector: tsparams.FRRK8SLabelSelector,
		})
		Expect(err).ToNot(HaveOccurred(), "Fail to list frr pods")
		Expect(len(frrPods)).To(BeNumerically(">", 0),
			"Failed the number of frr node pods is 0")

		createBGPPeerAndVerifyIfItsReady(
			ipv4metalLbIPList[0], "", tsparams.RemoteBGPASN, true, frrPods)

	})

	FIt("Verify that prefixes configured with alwaysBlock are not received by the FRR speakers",
		reportxml.ID("47270"), func() {
			By("Creating static ip annotation")

			By("Creating a new instance of MetalLB Speakers on workers")
			err := metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunningWithAlwaysBlock(tsparams.DefaultTimeout, workerLabelMap,
				[]string{"192.168.100.0/24", "2001:100::0/64"})
			Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")
			time.Sleep(10 * time.Minute)
			//staticIPAnnotation := pod.StaticIPAnnotation(
			//	externalNad.Definition.Name, []string{fmt.Sprintf("%s/%s", ipv4metalLbIPList[0], "24")})
			//
			//By("Creating MetalLb configMap")
			//masterConfigMap := createConfigMap(tsparams.LocalBGPASN, ipv4NodeAddrList, true, false)

			By("Creating FRR Pod")
			//frrPod := createFrrPod(
			//	masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, staticIPAnnotation)
			//
			//createBGPPeerAndVerifyIfItsReady(
			//	ipv4metalLbIPList[0], "", tsparams.LocalBGPASN, true, frrPods)

			//By("Checking that BGP session is established and up")
			//verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))

			By("Create a frrconfiguration allow all")
			frrConfig := metallb.NewFrrConfigurationBuilder(APIClient, "frrconfig-allow-all",
				NetConfig.MlbOperatorNamespace, ipv4metalLbIPList[0], 64500, 64501).
				WithConnectTime(metav1.Duration{Duration: 10 * time.Second}).WithEBGPMultiHop(true)

			_, err = frrConfig.WithBGPPassword("bgp-test").WithHoldTime(metav1.Duration{
				Duration: 90 * time.Second}).WithKeepalive(metav1.Duration{Duration: 30 * time.Second}).WithToReceiveModeAll("all").Create()
			Expect(err).ToNot(HaveOccurred(), "Fail to create a FRRconfiguration")
			time.Sleep(5 * time.Minute)
			By("Verify routes are being advertised from the external frr pod")

			By("Verify no routes are received on the frr node pods")

		})

	It("Verify the FRR node only receives routes that are configured in the allowed prefixes",
		reportxml.ID("47272"), func() {
			By("Creating static ip annotation")
			staticIPAnnotation := pod.StaticIPAnnotation(
				externalNad.Definition.Name, []string{fmt.Sprintf("%s/%s", ipv4metalLbIPList[0], "24")})

			By("Creating MetalLb configMap")
			masterConfigMap := createConfigMap(tsparams.LocalBGPASN, ipv4NodeAddrList, false, false)

			By("Creating FRR Pod")
			frrPod := createFrrPod(
				masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, staticIPAnnotation)

			createBGPPeerAndVerifyIfItsReady(
				ipv4metalLbIPList[0], "", tsparams.LocalBGPASN, false, frrPods)

			By("Checking that BGP session is established and up")
			verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))

			By("Create a frrconfiguration in filter mode with IPv4 prefixes")
			frrConfigIPv4 := metallb.NewFrrConfigurationBuilder(APIClient, "frrconfig-allowed-ipv4-prefixes",
				NetConfig.MlbOperatorNamespace, ipv4metalLbIPList[0], 64500, 64500)
			fmt.Println(frrConfigIPv4.Definition.Spec.BGP.Routers[0].Neighbors)
			neighborIPv4 := frrConfigIPv4.Definition.Spec.BGP.Routers[0].Neighbors
			prefixListIPv4 := []string{"192.168.100.0/24", "192.168.200.0/24"}
			_, err := frrConfigIPv4.WithBGPPassword("bgp-test").WithHoldTime(metav1.Duration{
				Duration: 90 * time.Second}).WithKeepalive(metav1.Duration{
				Duration: 30 * time.Second}).WithToReceiveModeFiltered(neighborIPv4[0], prefixListIPv4).Create()
			Expect(err).ToNot(HaveOccurred(), "Fail to create a FRRconfiguration for IPv4 prefixes")

			By("Create a frrconfiguration in filter mode with IPv6 prefixes")
			frrConfigIPv6 := metallb.NewFrrConfigurationBuilder(APIClient, "frrconfig-allowed-ipv6-prefixes",
				NetConfig.MlbOperatorNamespace, ipv6metalLbIPList[0], 64500, 64500)
			fmt.Println(frrConfigIPv6.Definition.Spec.BGP.Routers[0].Neighbors)
			neighborIPv6 := frrConfigIPv6.Definition.Spec.BGP.Routers[0].Neighbors
			prefixListIPv6 := []string{"2001:100::/64", "2001:200::/64"}
			fmt.Println("IPV6", neighborIPv6)
			_, err = frrConfigIPv6.WithBGPPassword("bgp-test").WithHoldTime(metav1.Duration{
				Duration: 90 * time.Second}).WithKeepalive(metav1.Duration{
				Duration: 30 * time.Second}).WithToReceiveModeFiltered(neighborIPv6[0], prefixListIPv6).Create()
			Expect(err).ToNot(HaveOccurred(), "Fail to create a FRRconfiguration for IPv6 prefixes")

			By("Verify routes are being advertised from the external frr pod")

			By("Verify no routes are received on the frr node pods")

		})

	It("Verify that when the allow all mode is configured all routes are received on the FRR speaker",
		reportxml.ID("47273"), func() {
			By("Creating static ip annotation")
			staticIPAnnotation := pod.StaticIPAnnotation(
				externalNad.Definition.Name, []string{fmt.Sprintf("%s/%s", ipv4metalLbIPList[0], "24")})

			By("Creating MetalLb configMap")
			masterConfigMap := createConfigMap(tsparams.LocalBGPASN, ipv4NodeAddrList, false, false)

			By("Creating FRR Pod")
			frrPod := createFrrPod(
				masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, staticIPAnnotation)

			createBGPPeerAndVerifyIfItsReady(
				ipv4metalLbIPList[0], "", tsparams.LocalBGPASN, false, frrPods)

			By("Checking that BGP session is established and up")
			verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))

			By("Create a frrconfiguration block all")
			frrConfig := metallb.NewFrrConfigurationBuilder(APIClient, "frrconfig-block-all",
				NetConfig.MlbOperatorNamespace, ipv4metalLbIPList[0], 64500, 64500)

			_, err := frrConfig.WithBGPPassword("bgp-test").WithHoldTime(metav1.Duration{
				Duration: 90 * time.Second}).WithKeepalive(metav1.Duration{
				Duration: 30 * time.Second}).Create()
			Expect(err).ToNot(HaveOccurred(), "Fail to create a FRRconfiguration")

			By("Verify routes are being advertised from the external frr pod")

			By("Verify no routes are received on the frr node pods")

		})

	It("Verify that when the allow all mode is configured all routes are received on the FRR speaker",
		reportxml.ID("47273"), func() {
			By("Creating static ip annotation")
			staticIPAnnotation := pod.StaticIPAnnotation(
				externalNad.Definition.Name, []string{fmt.Sprintf("%s/%s", ipv4metalLbIPList[0], "24")})

			By("Creating MetalLb configMap")
			masterConfigMap := createConfigMap(tsparams.LocalBGPASN, ipv4NodeAddrList, false, false)

			By("Creating FRR Pod")
			frrPod := createFrrPod(
				masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, staticIPAnnotation)

			createBGPPeerAndVerifyIfItsReady(
				ipv4metalLbIPList[0], "", tsparams.LocalBGPASN, false, frrPods)

			By("Checking that BGP session is established and up")
			verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))

			By("Create a frrconfiguration block all")
			frrConfig := metallb.NewFrrConfigurationBuilder(APIClient, "frrconfig-block-all",
				NetConfig.MlbOperatorNamespace, ipv4metalLbIPList[0], 64500, 64500)

			_, err := frrConfig.WithBGPPassword("bgp-test").WithHoldTime(metav1.Duration{
				Duration: 90 * time.Second}).WithKeepalive(metav1.Duration{
				Duration: 30 * time.Second}).Create()
			Expect(err).ToNot(HaveOccurred(), "Fail to create a FRRconfiguration")

			By("Verify routes are being advertised from the external frr pod")

			By("Verify no routes are received on the frr node pods")

		})

	AfterEach(func() {
		if len(cnfWorkerNodeList) > 2 {
			By("Remove custom metallb test label from nodes")
			removeNodeLabel(workerNodeList, metalLbTestsLabel)
		}

		By("Cleaning MetalLb operator namespace")
		metalLbNs, err := namespace.Pull(APIClient, NetConfig.MlbOperatorNamespace)
		Expect(err).ToNot(HaveOccurred(), "Failed to pull metalLb operator namespace")
		err = metalLbNs.CleanObjects(
			tsparams.DefaultTimeout,
			metallb.GetBGPPeerGVR(),
			metallb.GetBFDProfileGVR(),
			metallb.GetBGPPeerGVR(),
			metallb.GetBGPAdvertisementGVR(),
			metallb.GetFrrConfigurationGVR(),
			metallb.GetIPAddressPoolGVR())
		Expect(err).ToNot(HaveOccurred(), "Failed to remove object's from operator namespace")

		By("Cleaning test namespace")
		err = namespace.NewBuilder(APIClient, tsparams.TestNamespaceName).CleanObjects(
			tsparams.DefaultTimeout,
			pod.GetGVR(),
			service.GetServiceGVR(),
			configmap.GetGVR(),
			nad.GetGVR())
		Expect(err).ToNot(HaveOccurred(), "Failed to clean test namespace")
	})
})

func validateAdvertisedPrefix(
	masterNodeFRRPod *pod.Builder, ipProtoVersion string, workerNodesAddresses, addressPool []string, prefixLength int) {
	Eventually(
		frr.GetBGPStatus, time.Minute, tsparams.DefaultRetryInterval).
		WithArguments(masterNodeFRRPod, strings.ToLower(ipProtoVersion), "test").ShouldNot(BeNil())

	bgpStatus, err := frr.GetBGPStatus(masterNodeFRRPod, strings.ToLower(ipProtoVersion), "test")
	Expect(err).ToNot(HaveOccurred(), "Failed to verify bgp status")
	_, subnet, err := net.ParseCIDR(fmt.Sprintf("%s/%d", addressPool[0], prefixLength))
	Expect(err).ToNot(HaveOccurred(), "Failed to parse CIDR")
	Expect(bgpStatus.Routes).To(HaveKey(subnet.String()), "Failed to verify subnet in bgp status output")

	var nextHopAddresses []string

	for _, nextHop := range bgpStatus.Routes[subnet.String()] {
		Expect(nextHop.PrefixLen).To(BeNumerically("==", prefixLength),
			"Failed prefix length is not in expected value")

		for _, nHop := range nextHop.Nexthops {
			nextHopAddresses = append(nextHopAddresses, nHop.IP)
		}
	}

	Expect(workerNodesAddresses).To(ContainElements(nextHopAddresses),
		"Failed next hop address in not in node addresses list")

	_, err = frr.GetBGPCommunityStatus(masterNodeFRRPod, strings.ToLower(ipProtoVersion))
	Expect(err).ToNot(HaveOccurred(), "Failed to collect bgp community status")
}
