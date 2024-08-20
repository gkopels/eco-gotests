package tests

import (
	"context"
	"fmt"
	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/configmap"
	"github.com/openshift-kni/eco-goinfra/pkg/daemonset"
	"github.com/openshift-kni/eco-goinfra/pkg/metallb"
	"github.com/openshift-kni/eco-goinfra/pkg/nad"
	"github.com/openshift-kni/eco-goinfra/pkg/namespace"
	"github.com/openshift-kni/eco-goinfra/pkg/nodes"
	"github.com/openshift-kni/eco-goinfra/pkg/pod"
	"github.com/openshift-kni/eco-goinfra/pkg/reportxml"
	"github.com/openshift-kni/eco-goinfra/pkg/service"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netparam"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/frr"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/metallbenv"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/tsparams"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"strings"
	"time"
)

var _ = Describe("FRR", Ordered, Label(tsparams.LabelFRRTestCases), ContinueOnFailure, func() {
	BeforeAll(func() {
		var (
			err error
		)

		By("Getting MetalLb load balancer ip addresses")
		ipv4metalLbIPList, ipv6metalLbIPList, err = metallbenv.GetMetalLbIPByIPStack()
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

		err = metallbenv.IsEnvVarMetalLbIPinNodeExtNetRange(ipv4NodeAddrList, ipv4metalLbIPList, nil)
		Expect(err).ToNot(HaveOccurred(), "Failed to validate metalLb exported ip address")

		By("Listing master nodes")
		masterNodeList, err = nodes.List(APIClient,
			metav1.ListOptions{LabelSelector: labels.Set(NetConfig.ControlPlaneLabelMap).String()})
		Expect(err).ToNot(HaveOccurred(), "Fail to list master nodes")
		Expect(len(masterNodeList)).To(BeNumerically(">", 0),
			"Failed to detect master nodes")
	})

	AfterEach(func() {
		By("Cleaning MetalLb operator namespace")
		metalLbNs, err := namespace.Pull(APIClient, NetConfig.MlbOperatorNamespace)
		Expect(err).ToNot(HaveOccurred(), "Failed to pull metalLb operator namespace")
		err = metalLbNs.CleanObjects(
			tsparams.DefaultTimeout,
			metallb.GetBGPPeerGVR(),
			metallb.GetBFDProfileGVR(),
			metallb.GetBGPPeerGVR(),
			metallb.GetBGPAdvertisementGVR(),
			metallb.GetIPAddressPoolGVR(),
			metallb.GetMetalLbIoGVR(),
			metallb.GetFrrConfigurationGVR())
		Expect(err).ToNot(HaveOccurred(), "Failed to remove object's from operator namespace")

		By("Cleaning test namespace")
		err = namespace.NewBuilder(APIClient, tsparams.TestNamespaceName).CleanObjects(
			tsparams.DefaultTimeout,
			pod.GetGVR(),
			service.GetServiceGVR(),
			configmap.GetGVR(),
			nad.GetGVR())
		metallb.GetFrrConfigurationGVR()
		Expect(err).ToNot(HaveOccurred(), "Failed to clean test namespace")
	})

	Context("IBGP Single hop", func() {

		var (
			nodeAddrList []string
			addressPool  []string
			err          error
			frrk8sPods   []*pod.Builder
		)

		BeforeAll(func() {
			By("Setting test iteration parameters")
			_, _, _, nodeAddrList, addressPool, _, err =
				metallbenv.DefineIterationParams(
					ipv4metalLbIPList, ipv6metalLbIPList, ipv4NodeAddrList, ipv6NodeAddrList, "IPv4")
			Expect(err).ToNot(HaveOccurred(), "Fail to set iteration parameters")

		})

		It("Verify that prefixes configured with alwaysBlock are not received by the FRR speakers",
			reportxml.ID("47270"), func() {
				By("Creating a new instance of MetalLB Speakers on workers blocking specific incoming prefixes")
				err := createNewMetalLbDaemonSetAndWaitUntilItsRunningWithAlwaysBlock(tsparams.DefaultTimeout, workerLabelMap, []string{"192.168.100.0/24"})
				Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

				frrPod := deployTestPods(addressPool)

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady(ipv4metalLbIPList[0], "", 64500, false, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))
				time.Sleep(30 * time.Second)
				By("Validating BGP route prefix")
				validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create a frrconfiguration allow all")
				_, err = createFrrConfiguration("frrconfig-allow-all", ipv4metalLbIPList[0], 64500, 64500, nil, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a FRRconfiguration with allow all")

				nodeIPaddresses := []string{"10.46.81.2", "10.46.81.3"}

				frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
					LabelSelector: "app=frr-k8s",
				})

				By("Verify that the node FRR pods advertises two routes")

				advertisedRoutes, err := frr.GetBGPAdvertisedRoutes(frrPod, nodeIPaddresses)
				Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")

				actualRoutes := strings.Split(strings.TrimSpace(advertisedRoutes), "\n")
				expectedRoutes := []string{"192.168.100.0/24", "192.168.200.0/24"}

				// Check that the actual routes contain all the expected routes
				for _, expectedRoute := range expectedRoutes {
					Expect(actualRoutes).To(ContainElement(expectedRoute), fmt.Sprintf("Expected route %s not found", expectedRoute))
				}

				By("Validate BGP received routes")
				Eventually(func() string {
					// Get the routes
					routes, err := frr.VerifyBGPReceivedRoutes(frrk8sPods)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")

					// Return the routes to be checked
					return routes
				}, 60*time.Second, 5*time.Second).Should(ContainSubstring("192.168.200.0/24"), "Fail to find received routes")
			})

		It("Verify the FRR node only receives routes that are configured in the allowed prefixes",
			reportxml.ID("47272"), func() {

				By("Creating a new instance of MetalLB Speakers on workers")
				err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
				Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

				frrPod := deployTestPods(addressPool)

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady(ipv4metalLbIPList[0], "", 64500, false, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))
				time.Sleep(30 * time.Second)
				By("Validating BGP route prefix")
				validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create a frrconfiguration with prefix filter")
				_, err := createFrrConfiguration("frrconfig-filtered", ipv4metalLbIPList[0], 64500,
					64500, []string{"192.168.100.0/24", "2001:100::0/64"}, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a FRRconfiguration")

				nodeIPaddresses := []string{"10.46.81.2", "10.46.81.3"}

				frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
					LabelSelector: "app=frr-k8s",
				})

				By("Verify that the node FRR pods advertises two routes")

				advertisedRoutes, err := frr.GetBGPAdvertisedRoutes(frrPod, nodeIPaddresses)
				Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")

				actualRoutes := strings.Split(strings.TrimSpace(advertisedRoutes), "\n")
				expectedRoutes := []string{"192.168.100.0/24", "192.168.200.0/24"}

				// Check that the actual routes contain all the expected routes
				for _, expectedRoute := range expectedRoutes {
					Expect(actualRoutes).To(ContainElement(expectedRoute), fmt.Sprintf("Expected route %s not found", expectedRoute))
				}

				By("Validate BGP received routes")
				Eventually(func() string {
					// Get the routes
					routes, err := frr.VerifyBGPReceivedRoutes(frrk8sPods)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")

					// Return the routes to be checked
					return routes
				}, 60*time.Second, 5*time.Second).Should(SatisfyAll(
					ContainSubstring("192.168.100.0/24"),      // First IP address
					Not(ContainSubstring("192.168.200.0/24")), // Second IP address
				), "Fail to find all expected received routes")
			})

		It("Verify that when the allow all mode is configured all routes are received on the FRR speaker",
			reportxml.ID("47273"), func() {

				By("Creating a new instance of MetalLB Speakers on workers")
				err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
				Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

				frrPod := deployTestPods(addressPool)

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady(ipv4metalLbIPList[0], "", 64500, false, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))
				time.Sleep(30 * time.Second)
				By("Validating BGP route prefix")
				validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create a frrconfiguration allow all")
				_, err = createFrrConfiguration("frrconfig-allow-all", ipv4metalLbIPList[0],
					64500, 64500, nil, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a FRRconfiguration with allow all")

				nodeIPaddresses := []string{"10.46.81.2", "10.46.81.3"}

				frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
					LabelSelector: "app=frr-k8s",
				})

				By("Verify that the node FRR pods advertises two routes")
				time.Sleep(30 * time.Second)
				advertisedRoutes, err := frr.GetBGPAdvertisedRoutes(frrPod, nodeIPaddresses)
				Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")

				actualRoutes := strings.Split(strings.TrimSpace(advertisedRoutes), "\n")
				expectedRoutes := []string{"192.168.100.0/24", "192.168.200.0/24"}

				// Check that the actual routes contain all the expected routes
				for _, expectedRoute := range expectedRoutes {
					Expect(actualRoutes).To(ContainElement(expectedRoute), fmt.Sprintf("Expected route %s not found", expectedRoute))
				}

				By("Validate BGP received routes")
				Eventually(func() string {
					// Get the routes
					routes, err := frr.VerifyBGPReceivedRoutes(frrk8sPods)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")

					// Return the routes to be checked
					return routes
				}, 60*time.Second, 5*time.Second).Should(SatisfyAll(
					ContainSubstring("192.168.100.0/24"), // First IP address
					ContainSubstring("192.168.200.0/24"), // Second IP address
				), "Fail to find all expected received routes")
			})

		It("Verify that a FRR speaker can be updated by merging two different FRRConfigurations",
			reportxml.ID("74274"), func() {

				By("Creating a new instance of MetalLB Speakers on workers")
				err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
				Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

				frrPod := deployTestPods(addressPool)

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady(ipv4metalLbIPList[0], "", 64500, false, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))
				time.Sleep(30 * time.Second)
				By("Validating BGP route prefix")
				validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create first frrconfiguration that receieves a single route")
				_, err := createFrrConfiguration("frrconfig-filtered-1", ipv4metalLbIPList[0],
					64500, 64500, []string{"192.168.100.0/24", "2001:100::0/64"}, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a first FRRconfiguration")

				nodeIPaddresses := []string{"10.46.81.2", "10.46.81.3"}

				frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
					LabelSelector: "app=frr-k8s",
				})

				By("Verify that the node FRR pods advertises two routes")
				time.Sleep(30 * time.Second)
				advertisedRoutes, err := frr.GetBGPAdvertisedRoutes(frrPod, nodeIPaddresses)
				Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")

				actualRoutes := strings.Split(strings.TrimSpace(advertisedRoutes), "\n")
				expectedRoutes := []string{"192.168.100.0/24", "192.168.200.0/24"}

				// Check that the actual routes contain all the expected routes
				for _, expectedRoute := range expectedRoutes {
					Expect(actualRoutes).To(ContainElement(expectedRoute), fmt.Sprintf("Expected route %s not found", expectedRoute))
				}

				By("Validate BGP received only the first route")
				Eventually(func() string {
					// Get the routes
					routes, err := frr.VerifyBGPReceivedRoutes(frrk8sPods)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")

					// Return the routes to be checked
					return routes
				}, 60*time.Second, 5*time.Second).Should(SatisfyAll(
					ContainSubstring("192.168.100.0/24"),      // First IP address
					Not(ContainSubstring("192.168.200.0/24")), // Second IP address
				), "Fail to find all expected received routes")

				By("Create second frrconfiguration that receives a single route")
				_, err = createFrrConfiguration("frrconfig-filtered-2", ipv4metalLbIPList[0],
					64500, 64500, []string{"192.168.200.0/24", "2001:200::0/64"}, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a second FRRconfiguration")

				By("Validate BGP received only the first route")
				Eventually(func() string {
					// Get the routes
					routes, err := frr.VerifyBGPReceivedRoutes(frrk8sPods)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")

					// Return the routes to be checked
					return routes
				}, 60*time.Second, 5*time.Second).Should(SatisfyAll(
					ContainSubstring("192.168.100.0/24"), // First IP address
					ContainSubstring("192.168.200.0/24"), // Second IP address
				), "Fail to find all expected received routes")
			})

		It("Verify that a FRR speaker rejects a contrasting FRRConfiguration merge",
			reportxml.ID("74275"), func() {

				By("Creating a new instance of MetalLB Speakers on workers")
				err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
				Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

				frrPod := deployTestPods(addressPool)

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady(ipv4metalLbIPList[0], "", 64500, false, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))
				time.Sleep(30 * time.Second)
				By("Validating BGP route prefix")
				validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create first frrconfiguration that receieves a single route")
				_, err := createFrrConfiguration("frrconfig-filtered-1", ipv4metalLbIPList[0],
					64500, 64500, []string{"192.168.100.0/24", "2001:100::0/64"}, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a first FRRconfiguration")

				nodeIPaddresses := []string{"10.46.81.2", "10.46.81.3"}

				frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
					LabelSelector: "app=frr-k8s",
				})

				By("Verify that the node FRR pods advertises two routes")
				time.Sleep(30 * time.Second)
				advertisedRoutes, err := frr.GetBGPAdvertisedRoutes(frrPod, nodeIPaddresses)
				Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")

				actualRoutes := strings.Split(strings.TrimSpace(advertisedRoutes), "\n")
				expectedRoutes := []string{"192.168.100.0/24", "192.168.200.0/24"}

				// Check that the actual routes contain all the expected routes
				for _, expectedRoute := range expectedRoutes {
					Expect(actualRoutes).To(ContainElement(expectedRoute), fmt.Sprintf("Expected route %s not found", expectedRoute))
				}
				time.Sleep(30 * time.Minute)
				By("Validate BGP received routes")
				Eventually(func() string {
					// Get the routes
					routes, err := frr.VerifyBGPReceivedRoutes(frrk8sPods)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")

					// Return the routes to be checked
					return routes
				}, 60*time.Second, 5*time.Second).Should(SatisfyAll(
					ContainSubstring("192.168.100.0/24"),      // First IP address
					Not(ContainSubstring("192.168.200.0/24")), // Second IP address
				), "Fail to find all expected received routes")

				By("Create second frrconfiguration with an incorrect AS configuration")
				_, err = createFrrConfiguration("frrconfig-filtered-2", ipv4metalLbIPList[0],
					64500, 64501, []string{"192.168.200.0/24", "2001:200::0/64"}, false)
				Expect(err).To(HaveOccurred(), "successful created the FRRconfiguration and expected to fail")
			})

		FIt("Verify that the BGP status is correctly updated in the FRRNodeState",
			reportxml.ID("74280"), func() {

				By("Creating a new instance of MetalLB Speakers on workers")
				err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
				Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady(ipv4metalLbIPList[0], "", 64500, false, frrk8sPods)

				By("Create first frrconfiguration that receieves a single route")
				_, err := createFrrConfiguration("frrconfig-filtered-1", ipv4metalLbIPList[0],
					64500, 64500, []string{"192.168.100.0/24", "2001:100::0/64"}, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a first FRRconfiguration")

				By("Verify Worker-0 Node state update")
				Eventually(func() string {
					// Get the routes
					frrNodeState, err := metallb.ListFrrNodeState(APIClient)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")
					fmt.Println("frrNodeState[0].Objects.Status.RunningConfig", frrNodeState[0].Object.Status.RunningConfig)
					return frrNodeState[0].Object.Status.RunningConfig

					// Return the routes to be checked
				}, 60*time.Second, 5*time.Second).Should(SatisfyAll(
					ContainSubstring("permit 192.168.100.0/24"),      // First IP address
					Not(ContainSubstring("permit 192.168.200.0/24")), // Second IP address
				), "Fail to find all expected received routes")

				By("Create second frrconfiguration that receives a single route")
				_, err = createFrrConfiguration("frrconfig-filtered-2", ipv4metalLbIPList[0],
					64500, 64500, []string{"192.168.200.0/24", "2001:200::0/64"}, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a second FRRconfiguration")

				By("Verify Worker-1 Node state update")
				Eventually(func() string {
					// Get the routes
					frrNodeState, err := metallb.ListFrrNodeState(APIClient)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")
					fmt.Println("frrNodeState[0].Objects.Status.RunningConfig", frrNodeState[0].Object.Status.RunningConfig)
					return frrNodeState[0].Object.Status.RunningConfig

					// Return the routes to be checked
				}, 60*time.Second, 5*time.Second).Should(SatisfyAll(
					ContainSubstring("permit 192.168.100.0/24"), // First IP address
					ContainSubstring("permit 192.168.200.0/24"), // Second IP address
				), "Fail to find all expected received routes")
			})
	})

	Context("BGP Multihop", func() {
		//var speakerRoutesMap map[string]string
		BeforeAll(func() {
			By("Creating a new instance of MetalLB Speakers on workers")
			err := metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
			Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

			frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
				LabelSelector: "app=frr-k8s",
			})
			Expect(err).ToNot(HaveOccurred(), "Fail to list speaker pods")

			By("Collecting information before test")

			Expect(err).ToNot(HaveOccurred(), "Failed to list speaker pods")
			By("Setting test iteration parameters")
			masterClientPodIP, _, _, _, addressPool, _, err :=
				metallbenv.DefineIterationParams(
					ipv4metalLbIPList, ipv6metalLbIPList, ipv4NodeAddrList, ipv6NodeAddrList, "IPv4")
			Expect(err).ToNot(HaveOccurred(), "Fail to set iteration parameters")

			By("Adding static routes to the speakers")
			speakerRoutesMap, err := buildRoutesMap(frrk8sPods, ipv4metalLbIPList)
			Expect(err).ToNot(HaveOccurred(), "Failed to build speaker route map")

			for _, frrk8sPod := range frrk8sPods {
				out, err := frr.SetStaticRoute(frrk8sPod, "add", masterClientPodIP, speakerRoutesMap)
				Expect(err).ToNot(HaveOccurred(), out)
			}

			By("Creating an IPAddressPool and BGPAdvertisement")

			ipAddressPool := setupBgpAdvertisement(addressPool, int32(32))

			By("Creating a MetalLB service")
			setupMetalLbService("IPv4", ipAddressPool, "Cluster")

			By("Creating nginx test pod on worker node")
			setupNGNXPod(workerNodeList[0].Definition.Name)

			By("Creating External NAD for master FRR pod")
			createExternalNad(tsparams.ExternalMacVlanNADName)

			By("Creating External NAD for hub FRR pods")
			createExternalNad(tsparams.HubMacVlanNADName)

			By("Creating static ip annotation for hub0")
			hub0BRstaticIPAnnotation := pod.StaticIPAnnotation("external", []string{"10.46.81.131/24", "2001:81:81::1/64"})
			hub0BRstaticIPAnnotation = append(hub0BRstaticIPAnnotation,
				pod.StaticIPAnnotation(tsparams.HubMacVlanNADName, []string{"172.16.0.10/24", "2001:100:100::10/64"})...)

			By("Creating static ip annotation for hub1")
			hub1BRstaticIPAnnotation := pod.StaticIPAnnotation("external", []string{"10.46.81.132/24", "2001:81:81::2/64"})
			hub1BRstaticIPAnnotation = append(hub1BRstaticIPAnnotation,
				pod.StaticIPAnnotation(tsparams.HubMacVlanNADName, []string{"172.16.0.11/24", "2001:100:100::11/64"})...)

			By("Creating MetalLb Hub pod configMap")
			hubConfigMap := createHubConfigMap("frr-hub-node-config")

			By("Creating FRR Hub Worker-0 Pod")
			_ = createFrrHubPod("hub-pod-worker-0",
				workerNodeList[0].Object.Name, hubConfigMap.Definition.Name, []string{}, hub0BRstaticIPAnnotation)

			By("Creating FRR Hub Worker-1 Pod")
			_ = createFrrHubPod("hub-pod-worker-1",
				workerNodeList[1].Object.Name, hubConfigMap.Definition.Name, []string{}, hub1BRstaticIPAnnotation)
		})

		AfterAll(func() {
			//By("Removing static routes from the speakers")
			//frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
			//	LabelSelector: "app=frr-k8s",
			//})
			//Expect(err).ToNot(HaveOccurred(), "Failed to list pods")
			//
			//for _, frrk8sPod := range frrk8sPods {
			//	out, err := frr.SetStaticRoute(frrk8sPod, "del", "172.16.0.1", speakerRoutesMap)
			//	Expect(err).ToNot(HaveOccurred(), out)
			//}
		})

		It("Validate a FRR node receives and sends IPv4 and IPv6 routes from an IBGP multihop FRR instance",
			reportxml.ID("47278"), func() {

				By("Collecting information before test")
				frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
					LabelSelector: "app=frr-k8s",
				})
				Expect(err).ToNot(HaveOccurred(), "Failed to list speaker pods")
				By("Setting test iteration parameters")
				_, _, _, nodeAddrList, addressPool, _, err :=
					metallbenv.DefineIterationParams(
						ipv4metalLbIPList, ipv6metalLbIPList, ipv4NodeAddrList, ipv6NodeAddrList, "IPv4")
				Expect(err).ToNot(HaveOccurred(), "Fail to set iteration parameters")

				By("Creating MetalLb Master pod configMap")
				frrPod := createMasterFrrPod(64500, nodeAddrList, false)

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady("172.16.0.1", "", tsparams.LocalBGPASN, false, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))

				By("Validating BGP route prefix")
				validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create a frrconfiguration allow all for IBGP multihop")
				_, err = createFrrConfiguration("frrconfig-allow-all", "172.16.0.1",
					64500, 64500, nil, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create an allow-all FRRconfiguration")

				nodeIPaddresses := []string{"10.46.81.2", "10.46.81.3"}

				By("Verify that the node FRR pods advertises two routes")

				advertisedRoutes, err := frr.GetBGPAdvertisedRoutes(frrPod, nodeIPaddresses)
				Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")

				actualRoutes := strings.Split(strings.TrimSpace(advertisedRoutes), "\n")
				expectedRoutes := []string{"192.168.100.0/24", "192.168.200.0/24"}

				// Check that the actual routes contain all the expected routes
				for _, expectedRoute := range expectedRoutes {
					Expect(actualRoutes).To(ContainElement(expectedRoute), fmt.Sprintf("Expected route %s not found", expectedRoute))
				}
				time.Sleep(20 * time.Second)
				By("Validate BGP received routes")
				routes, err := frr.VerifyBGPReceivedRoutes(frrk8sPods)
				Expect(err).ToNot(HaveOccurred(), "Fail to find find route")
				Expect(routes).To(ContainSubstring("192.168.100.0/24"), "Fail to find received routes")

				routes, err = frr.VerifyBGPReceivedRoutes(frrk8sPods)
				Expect(err).ToNot(HaveOccurred(), "Fail to find find route")
				Expect(routes).To(ContainSubstring("192.168.200.0/24"), "Fail to find received routes")
			})

		It("Validate a FRR node receives and sends IPv4 and IPv6 routes from an EBGP multihop FRR instance",
			reportxml.ID("47279"), func() {

				By("Collecting information before test")
				frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
					LabelSelector: tsparams.FRRK8sDefaultLabel,
				})
				Expect(err).ToNot(HaveOccurred(), "Failed to list speaker pods")
				By("Setting test iteration parameters")
				_, _, _, nodeAddrList, addressPool, _, err :=
					metallbenv.DefineIterationParams(
						ipv4metalLbIPList, ipv6metalLbIPList, ipv4NodeAddrList, ipv6NodeAddrList, "IPv4")
				Expect(err).ToNot(HaveOccurred(), "Fail to set iteration parameters")

				By("Creating MetalLb Master pod configMap")
				frrPod := createMasterFrrPod(64501, nodeAddrList, true)

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady("172.16.0.1", "", 64501, true, frrk8sPods)

				//By("Checking that BGP session is established and up")
				//verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))
				time.Sleep(30 * time.Second)
				By("Validating BGP route prefix")
				validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create a frrconfiguration allow all for EBGP multihop")
				_, err = createFrrConfiguration("frrconfig-allow-all", "172.16.0.1",
					64500, 64501, nil, true)
				Expect(err).ToNot(HaveOccurred(), "Fail to create an allow-all FRRconfiguration")

				By("Verify that the node FRR pods advertises two routes")
				nodeIPaddresses := []string{"10.46.81.2", "10.46.81.3"}
				advertisedRoutes, err := frr.GetBGPAdvertisedRoutes(frrPod, nodeIPaddresses)
				Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")

				actualRoutes := strings.Split(strings.TrimSpace(advertisedRoutes), "\n")
				expectedRoutes := []string{"192.168.100.0/24", "192.168.200.0/24"}

				// Check that the actual routes contain all the expected routes
				for _, expectedRoute := range expectedRoutes {
					Expect(actualRoutes).To(ContainElement(expectedRoute), fmt.Sprintf("Expected route %s not found", expectedRoute))
				}

				By("Validate BGP received routes")
				routes, err := frr.VerifyBGPReceivedRoutes(frrk8sPods)
				Expect(err).ToNot(HaveOccurred(), "Fail to find find route")
				Expect(routes).To(ContainSubstring("192.168.100.0/24"), "Fail to find received routes")

				routes, err = frr.VerifyBGPReceivedRoutes(frrk8sPods)
				Expect(err).ToNot(HaveOccurred(), "Fail to find find route")
				Expect(routes).To(ContainSubstring("192.168.200.0/24"), "Fail to find received routes")
			})
		It("Verify Frrk8 iBGP multihop over a secondary interface",
			reportxml.ID("75248"), func() {

				//By("Setting test iteration parameters")
				//_, _, _, nodeAddrList, addressPool, _, err :=
				//	metallbenv.DefineIterationParams(
				//		ipv4metalLbIPList, ipv6metalLbIPList, ipv4NodeAddrList, ipv6NodeAddrList, "IPv4")
				//Expect(err).ToNot(HaveOccurred(), "Fail to set iteration parameters")

				//secInterface := nmstate.NewPolicyBuilder(APIClient, "secint", map[string]string{
				//	"kubernetes.io/hostname": "worker-0",
				//})
				//
				//secInterface.WithVlanInterfaceIP("ens8f0np0", 178, "172.16.0.254", "2001:100::254")
				//_, err := secInterface.Create()
				//Expect(err).ToNot(HaveOccurred(), "Fail to create secondary interface")
				//
				//fmt.Println(secInterface.Definition)

				time.Sleep(10 * time.Minute)

				//By("Collecting information before test")
				//frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
				//	LabelSelector: "app=frr-k8s",
				//})
				//Expect(err).ToNot(HaveOccurred(), "Failed to list speaker pods")
				//By("Setting test iteration parameters")
				//_, _, _, nodeAddrList, addressPool, _, err :=
				//	metallbenv.DefineIterationParams(
				//		ipv4metalLbIPList, ipv6metalLbIPList, ipv4NodeAddrList, ipv6NodeAddrList, "IPv4")
				//Expect(err).ToNot(HaveOccurred(), "Fail to set iteration parameters")

				//By("Creating MetalLb Master pod configMap")
				//frrPod := createMasterFrrPod(64500, nodeAddrList, false)
				//
				//By("Creating BGP Peers")
				//createBGPPeerAndVerifyIfItsReady("172.16.0.1", "", tsparams.LocalBGPASN, false, frrk8sPods)
				//
				//By("Checking that BGP session is established and up")
				//verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))
				//
				//By("Validating BGP route prefix")
				//validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)
				//
				//By("Create a frrconfiguration allow all for IBGP multihop")
				//_, err = createFrrConfiguration("frrconfig-allow-all", "172.16.0.1",
				//	64500, 64500, nil, false)
				//Expect(err).ToNot(HaveOccurred(), "Fail to create an allow-all FRRconfiguration")
				//
				//time.Sleep(30 * time.Second)
				//nodeIPaddresses := []string{"10.46.81.2", "10.46.81.3"}
				//
				//By("Verify that the node FRR pods advertises two routes")
				//
				//advertisedRoutes, err := frr.GetBGPAdvertisedRoutes(frrPod, nodeIPaddresses)
				//Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")
				//
				//actualRoutes := strings.Split(strings.TrimSpace(advertisedRoutes), "\n")
				//expectedRoutes := []string{"192.168.100.0/24", "192.168.200.0/24"}
				//
				//// Check that the actual routes contain all the expected routes
				//for _, expectedRoute := range expectedRoutes {
				//	Expect(actualRoutes).To(ContainElement(expectedRoute), fmt.Sprintf("Expected route %s not found", expectedRoute))
				//}
				//
				//By("Validate BGP received routes")
				//routes, err := frr.VerifyBGPReceivedRoutes(frrk8sPods)
				//Expect(err).ToNot(HaveOccurred(), "Fail to find find route")
				//Expect(routes).To(ContainSubstring("192.168.100.0/24"), "Fail to find received routes")
			})
	})
})

func createNewMetalLbDaemonSetAndWaitUntilItsRunningWithAlwaysBlock(timeout time.Duration,
	nodeLabel map[string]string, prefixes []string) error {
	glog.V(90).Infof("Verifying if metalLb daemonset is running")

	metalLbIo, err := metallb.Pull(APIClient, tsparams.MetalLbIo, NetConfig.MlbOperatorNamespace)

	if err == nil {
		glog.V(90).Infof("MetalLb daemonset is running. Removing daemonset.")

		_, err = metalLbIo.Delete()

		if err != nil {
			return err
		}
	}

	glog.V(90).Infof("Create new metalLb speaker's daemonSet.")

	metalLbIo = metallb.NewBuilder(
		APIClient, tsparams.MetalLbIo, NetConfig.MlbOperatorNamespace, nodeLabel)

	metalLbIo.WithFRRconfigAlwaysBlock(prefixes)
	_, err = metalLbIo.Create()

	if err != nil {
		return err
	}

	var metalLbDs *daemonset.Builder

	err = wait.PollUntilContextTimeout(
		context.TODO(), 3*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
			metalLbDs, err = daemonset.Pull(APIClient, tsparams.MetalLbDsName, NetConfig.MlbOperatorNamespace)
			if err != nil {
				glog.V(90).Infof("Error to pull daemonset %s namespace %s, retry",
					tsparams.MetalLbDsName, NetConfig.MlbOperatorNamespace)

				return false, nil
			}

			return true, nil
		})

	if err != nil {
		return err
	}

	glog.V(90).Infof("Waiting until the new metalLb daemonset is in Ready state.")

	if metalLbDs.IsReady(timeout) {
		return nil
	}

	return fmt.Errorf("metallb daemonSet is not ready")
}

func createTestClientWithMultiInterfaces(
	name string,
	configmapName string,
	sriovNetworkNet1 string,
	sriovNetworkNet2 string,
	nodeName string,
	ipAddresses []string) *pod.Builder {
	By(fmt.Sprintf("Define and run container %s", name))

	annotation, err := pod.StaticIPMultiNetDualStackAnnotation([]string{sriovNetworkNet1, sriovNetworkNet2}, ipAddresses)
	Expect(err).ToNot(HaveOccurred(), "Failed to define a 2 net dual stack nad annotation")

	frrContainer := pod.NewContainerBuilder(
		tsparams.FRRSecondContainerName, NetConfig.CnfNetTestContainer, tsparams.SleepCMD).
		WithSecurityCapabilities([]string{"NET_ADMIN", "NET_RAW", "SYS_ADMIN"}, true)

	frrCtr, err := frrContainer.GetContainerCfg()

	frrPod, err := pod.NewBuilder(APIClient, name, tsparams.TestNamespaceName,
		NetConfig.CnfNetTestContainer).DefineOnNode(nodeName).WithPrivilegedFlag().
		WithSecondaryNetwork(annotation).WithAdditionalContainer(frrCtr).WithLocalVolume(configmapName, "/etc/frr").CreateAndWaitUntilRunning(netparam.DefaultTimeout)
	Expect(err).ToNot(HaveOccurred(), "Failed to define and run default client")

	return frrPod
}

func deployTestPods(addressPool []string) *pod.Builder {

	var err error

	By("Creating an IPAddressPool and BGPAdvertisement")
	ipAddressPool := setupBgpAdvertisement(addressPool, int32(32))

	By("Creating a MetalLB service")
	setupMetalLbService("IPv4", ipAddressPool, "Cluster")

	By("Creating nginx test pod on worker node")
	setupNGNXPod(workerNodeList[0].Definition.Name)

	By("Creating External NAD")
	createExternalNad(tsparams.ExternalMacVlanNADName)

	By("Creating static ip annotation")
	staticIPAnnotation := pod.StaticIPAnnotation(
		externalNad.Definition.Name, []string{fmt.Sprintf("%s/%s", ipv4metalLbIPList[0], "24")})

	By("Creating MetalLb configMap")
	masterConfigMap := createConfigMap(tsparams.LocalBGPASN, ipv4NodeAddrList, false, false)

	By("Creating FRR Pod")
	frrPod := createFrrPod(
		masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, staticIPAnnotation)

	Eventually(func() error {
		return err
	}, time.Minute, tsparams.DefaultRetryInterval).ShouldNot(HaveOccurred(),
		"Failed to collect metrics from speaker pods")

	return frrPod
}

func createFrrConfiguration(name, bgpPeerIP string, localAS, remoteAS uint32, filteredIP []string, ebgp bool) (*metallb.FrrConfigurationBuilder, error) {
	frrConfig := metallb.NewFrrConfigurationBuilder(APIClient, name,
		NetConfig.MlbOperatorNamespace).WithBGPRouter(localAS).
		WithBGPNeighbor(bgpPeerIP, remoteAS, 0)
	if len(filteredIP) > 0 {
		frrConfig.WithToReceiveModeFiltered(filteredIP, 0, 0)
	} else {
		frrConfig.WithToReceiveModeAll(0, 0)
	}

	if ebgp == true {
		frrConfig.WithEBGPMultiHop(0, 0)
	}

	frrConfig.
		WithHoldTime(metav1.Duration{Duration: 90 * time.Second}, 0, 0).
		WithKeepalive(metav1.Duration{Duration: 30 * time.Second}, 0, 0).
		WithBGPPassword("bgp-test", 0, 0).
		WithPort(179, 0, 0)

	_, err := frrConfig.Create()

	if err != nil {
		glog.V(90).Infof("failed to create a frrconfiugration")

		return nil, err
	}

	return frrConfig, nil
}

func createMasterFrrPod(localAS int, ipv4NodeAddrList []string, ebgpMultiHop bool) *pod.Builder {
	masterConfigMap := createConfigMapWithStaticRoutes(localAS, ipv4NodeAddrList, ebgpMultiHop, false)

	By("Creating static ip annotation for master FRR pod")
	masterStaticIPAnnotation := pod.StaticIPAnnotation(
		tsparams.HubMacVlanNADName, []string{"172.16.0.1/24", "2001:100:100::254/64"})

	By("Creating FRR Master Pod")
	frrPod := createFrrPod(
		masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, masterStaticIPAnnotation)

	return frrPod
}
