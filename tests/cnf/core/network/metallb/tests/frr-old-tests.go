package tests

import (
	"fmt"
	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/configmap"
	"github.com/openshift-kni/eco-goinfra/pkg/metallb"
	"github.com/openshift-kni/eco-goinfra/pkg/nad"
	"github.com/openshift-kni/eco-goinfra/pkg/namespace"
	"github.com/openshift-kni/eco-goinfra/pkg/pod"
	"github.com/openshift-kni/eco-goinfra/pkg/reportxml"
	"github.com/openshift-kni/eco-goinfra/pkg/service"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/frr"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/tsparams"
	"net"
	"strings"
	"time"
)

var _ = Describe("FRR", Ordered, Label(tsparams.LabelFRRTestCases), ContinueOnFailure, func() {

	BeforeAll(func() {
		// var err error
		//By("Getting MetalLb load balancer ip addresses")
		//ipv4metalLbIPList, ipv6metalLbIPList, err = metallbenv.GetMetalLbIPByIPStack()
		//Expect(err).ToNot(HaveOccurred(), "An unexpected error occurred while "+
		//	"determining the IP addresses from the ECO_CNF_CORE_NET_MLB_ADDR_LIST environment variable.")
		//
		//By("Getting external nodes ip addresses")
		//cnfWorkerNodeList, err = nodes.List(APIClient,
		//	metav1.ListOptions{LabelSelector: labels.Set(NetConfig.WorkerLabelMap).String()})
		//Expect(err).ToNot(HaveOccurred(), "Failed to discover worker nodes")
		//
		//By("Selecting worker node for BGP tests")
		//workerLabelMap, workerNodeList = setWorkerNodeListAndLabelForBfdTests(cnfWorkerNodeList, metalLbTestsLabel)
		//ipv4NodeAddrList, err = nodes.ListExternalIPv4Networks(
		//	APIClient, metav1.ListOptions{LabelSelector: labels.Set(workerLabelMap).String()})
		//Expect(err).ToNot(HaveOccurred(), "Failed to collect external nodes ip addresses")
		//
		//By("Creating a new instance of MetalLB Speakers on workers")
		//err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
		//Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")
		//
		//err = metallbenv.IsEnvVarMetalLbIPinNodeExtNetRange(ipv4NodeAddrList, ipv4metalLbIPList, nil)
		//Expect(err).ToNot(HaveOccurred(), "Failed to validate metalLb exported ip address")
		//
		//By("Listing master nodes")
		//masterNodeList, err = nodes.List(APIClient,
		//	metav1.ListOptions{LabelSelector: labels.Set(NetConfig.ControlPlaneLabelMap).String()})
		//Expect(err).ToNot(HaveOccurred(), "Fail to list master nodes")
		//Expect(len(masterNodeList)).To(BeNumerically(">", 0),
		//	"Failed to detect master nodes")
	})

	var (
	//frrk8sPods []*pod.Builder
	//ipStack    string
	// prefixLen  int
	)

	BeforeEach(func() {
		//By("Creating External NAD")
		//createExternalNad()
		//
		//By("Listing metalLb speakers pod")
		//var err error
		//frrk8sPods, err = pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
		//	LabelSelector: tsparams.FRRK8sDefaultLabel,
		//})
		//Expect(err).ToNot(HaveOccurred(), "Fail to list speaker pods")
		//// time.Sleep(30 * time.Second)
		//// Expect(len(frrk8sPods)).To(BeNumerically(">", 0),
		////	"Failed the number of frr speaker pods is 0")
		//createBGPPeerAndVerifyIfItsReady(
		//	ipv4metalLbIPList[0], "", tsparams.LocalBGPASN, false, frrk8sPods)
		//
		//if ipStack == netparam.IPV6Family {
		//	Skip("bgp test cases doesn't support ipv6 yet")
		//}
		//
		//createBGPPeerAndVerifyIfItsReady(
		//	ipv4metalLbIPList[0], "", tsparams.LocalBGPASN, false, frrk8sPods)
		//
		//By("Setting test iteration parameters")
		//_, subMask, mlbAddressList, nodeAddrList, addressPool, _, err :=
		//	metallbenv.DefineIterationParams(
		//		ipv4metalLbIPList, ipv6metalLbIPList, ipv4NodeAddrList, ipv6NodeAddrList, "IPv4")
		//Expect(err).ToNot(HaveOccurred(), "Fail to set iteration parameters")
		//
		//By("Creating MetalLb configMap")
		//masterConfigMap := createConfigMap(tsparams.LocalBGPASN, nodeAddrList, false, false)
		//
		//By("Creating static ip annotation")
		//staticIPAnnotation := pod.StaticIPAnnotation(
		//	externalNad.Definition.Name, []string{fmt.Sprintf("%s/%s", mlbAddressList[0], subMask)})
		//
		//By("Creating FRR Pod")
		//_ = createFrrPod(
		//	masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, staticIPAnnotation)
		//
		//By("Creating an IPAddressPool and BGPAdvertisement")
		//ipAddressPool := setupBgpAdvertisement(addressPool, int32(32))
		//
		//By("Creating a MetalLB service")
		//setupMetalLbService(ipStack, ipAddressPool, "Cluster")
		//
		//By("Creating nginx test pod on worker node")
		//setupNGNXPod(workerNodeList[0].Definition.Name)

	})

	//It("Verify that prefixes configured with alwaysBlock are not received by the FRR speakers",
	//	reportxml.ID("47270"), func() {
	//		By("Creating a new instance of MetalLB Speakers on workers blocking specific incoming prefixes")
	//		err := metallbenv.CreateNewMetalLbDaemonSetWithBlockAll(tsparams.DefaultTimeout, workerLabelMap, []string{"192.168.100.0/24"})
	//		Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")
	//
	//	})

	It("Verify the FRR node only receives routes that are configured in the allowed prefixes",
		reportxml.ID("47272"), func() {
			By("Creating a new instance of MetalLB Speakers on workers")
			//err := metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
			//Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

			By("Create a frrconfiguration allow all")
			frrConfig := metallb.NewFrrConfigurationBuilder(APIClient, "frrconfig-allow-all",
				NetConfig.MlbOperatorNamespace).WithBGPRouter(64500)
			frrConfig.WithBGPNeighbor("10.46.73.131", 64550, 0).
				WithToReceiveModeFiltered([]string{"192.168.100.0/24", "2001:100::0/64"}, 0, 0)
			frrConfig.WithBGPNeighbor("10.46.73.132", 64550, 0).
				WithToReceiveModeFiltered([]string{"192.168.200.0/24", "2001:200::0/64"}, 0, 1)
			//WithBGPPassword("bgp-test", 0).
			//WithHoldTime(metav1.Duration{Duration: 90 * time.Second}, 0).WithKeepalive(metav1.Duration{Duration: 30 * time.Second}, 0).
			//WithConnectTime(metav1.Duration{Duration: 10 * time.Second}, 0)

			_, err := frrConfig.Create()
			Expect(err).ToNot(HaveOccurred(), "Fail to create a FRRconfiguration")
			//time.Sleep(20 * time.Minute)

			//_, err = frrConfig.WithBGPPassword("bgp-test").WithHoldTime(metav1.Duration{
			//	Duration: 90 * time.Second}).WithKeepalive(metav1.Duration{Duration: 30 * time.Second}).WithToReceiveModeAll("all").Create()

			//time.Sleep(5 * time.Minute)
			By("Verify routes are being advertised from the external frr pod")

			By("Verify no routes are received on the frr node pods")

		})

	It("Verify that configured with allowAll FRR speakers receive all advertised routes",
		reportxml.ID("47270"), func() {
			//var (
			//	ipStack    string
			//	prefixLen  int
			//	frrk8sPods []*pod.Builder
			//)

			//By("Creating a new instance of MetalLB Speakers on workers")
			//err := metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
			//Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

			//frrk8sPods, err = pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
			//	LabelSelector: tsparams.FRRK8sDefaultLabel,
			//})
			//if ipStack == netparam.IPV6Family {
			//	Skip("bgp test cases doesn't support ipv6 yet")
			//}
			//
			//createBGPPeerAndVerifyIfItsReady(
			//	ipv4metalLbIPList[0], "", tsparams.LocalBGPASN, false, frrk8sPods)
			//
			//By("Setting test iteration parameters")
			//_, subMask, mlbAddressList, nodeAddrList, addressPool, _, err :=
			//	metallbenv.DefineIterationParams(
			//		ipv4metalLbIPList, ipv6metalLbIPList, ipv4NodeAddrList, ipv6NodeAddrList, ipStack)
			//Expect(err).ToNot(HaveOccurred(), "Fail to set iteration parameters")
			//
			//By("Creating MetalLb configMap")
			//masterConfigMap := createConfigMap(tsparams.LocalBGPASN, nodeAddrList, false, false)
			//
			//By("Creating static ip annotation")
			//staticIPAnnotation := pod.StaticIPAnnotation(
			//	externalNad.Definition.Name, []string{fmt.Sprintf("%s/%s", mlbAddressList[0], subMask)})
			//
			//By("Creating FRR Pod")
			//frrPod := createFrrPod(
			//	masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, staticIPAnnotation)
			//
			//By("Creating an IPAddressPool and BGPAdvertisement")
			//ipAddressPool := setupBgpAdvertisement(addressPool, int32(32))
			//
			//By("Creating a MetalLB service")
			//setupMetalLbService(ipStack, ipAddressPool, "Cluster")
			//
			//By("Creating nginx test pod on worker node")
			//setupNGNXPod(workerNodeList[0].Definition.Name)

			By("Create a frrconfiguration allow all")
			frrConfig := metallb.NewFrrConfigurationBuilder(APIClient, "frrconfig-allow-all",
				NetConfig.MlbOperatorNamespace).WithBGPRouter(64500)
			frrConfig.WithBGPNeighbor(ipv4metalLbIPList[0], 64551, 0).
				WithToReceiveModeAll(0, 0).
				WithBGPPassword("bgp-password", 0, 0).
				WithEBGPMultiHop(0, 0)
			frrConfig.WithBGPNeighbor(ipv4metalLbIPList[1], 64551, 0).
				WithToReceiveModeAll(0, 1).
				WithBGPPassword("bgp-test", 0, 1).
				WithEBGPMultiHop(0, 1)
			_, err := frrConfig.Create()
			Expect(err).ToNot(HaveOccurred(), "Fail to create a FRRconfiguration")

			//By("Checking that BGP session is established and up")
			//verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(nodeAddrList))
			//
			//By("Validating BGP route prefix")
			//validatePrefix(frrPod, ipStack, removePrefixFromIPList(nodeAddrList), addressPool, prefixLen)

			// time.Sleep(5 * time.Minute)

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

//func CreateNewMetalLbDaemonSetAndWaitUntilItsRunningWithAlwaysBlock(timeout time.Duration,
//	nodeLabel map[string]string, prefixes []string) error {
//	glog.V(90).Infof("Verifying if metalLb daemonset is running")
//
//	metalLbIo, err := metallb.Pull(APIClient, tsparams.MetalLbIo, NetConfig.MlbOperatorNamespace)
//
//	if err == nil {
//		glog.V(90).Infof("MetalLb daemonset is running. Removing daemonset.")
//
//		_, err = metalLbIo.Delete()
//
//		if err != nil {
//			return err
//		}
//	}
//
//	glog.V(90).Infof("Create new metalLb speaker's daemonSet.")
//
//	metalLbIo = metallb.NewBuilder(
//		APIClient, tsparams.MetalLbIo, NetConfig.MlbOperatorNamespace, nodeLabel)
//
//	metalLbIo.WithFRRconfigAlwaysBlock(prefixes)
//	_, err = metalLbIo.Create()
//
//	if err != nil {
//		return err
//	}
//
//	var metalLbDs *daemonset.Builder
//
//	err = wait.PollUntilContextTimeout(
//		context.TODO(), 3*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
//			metalLbDs, err = daemonset.Pull(APIClient, tsparams.MetalLbDsName, NetConfig.MlbOperatorNamespace)
//			if err != nil {
//				glog.V(90).Infof("Error to pull daemonset %s namespace %s, retry",
//					tsparams.MetalLbDsName, NetConfig.MlbOperatorNamespace)
//
//				return false, nil
//			}
//
//			return true, nil
//		})
//
//	if err != nil {
//		return err
//	}
//
//	glog.V(90).Infof("Waiting until the new metalLb daemonset is in Ready state.")
//
//	if metalLbDs.IsReady(timeout) {
//		return nil
//	}
//
//	return fmt.Errorf("metallb daemonSet is not ready")
//}

func createMetallbDameonSet() error {
	metalLbIo, err := metallb.Pull(APIClient, tsparams.MetalLbIo, NetConfig.MlbOperatorNamespace)

	if err == nil {
		glog.V(90).Infof("MetalLb daemonset is running. Removing daemonset.")

		_, err = metalLbIo.Delete()

		if err != nil {
			return err
		}
	}

	return fmt.Errorf("metallb daemonSet is not ready")
}
