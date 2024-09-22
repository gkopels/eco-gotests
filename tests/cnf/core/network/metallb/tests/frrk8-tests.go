package tests

import (
	"context"
	"fmt"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netnmstate"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netparam"
	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	"strings"
	"time"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/configmap"
	"github.com/openshift-kni/eco-goinfra/pkg/daemonset"
	"github.com/openshift-kni/eco-goinfra/pkg/metallb"
	"github.com/openshift-kni/eco-goinfra/pkg/nad"
	"github.com/openshift-kni/eco-goinfra/pkg/namespace"
	"github.com/openshift-kni/eco-goinfra/pkg/nmstate"
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
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	externalAdvertisedIPv4Routes = []string{"192.168.100.0/24", "192.168.200.0/24"}
	externalAdvertisedIPv6Routes = []string{"2001:100::0/64", "2001:200::0/64"}
	hubIPaddresses               = []string{"172.16.0.10", "172.16.0.11"}
)

var _ = Describe("FRR", Ordered, Label(tsparams.LabelFRRTestCases), ContinueOnFailure, func() {
	BeforeAll(func() {
		var err error

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

		AfterEach(func() {
			By("Clean metallb operator and test namespaces")
			resetOperatorAndTestNS()
		})

		It("Verify that prefixes configured with alwaysBlock are not received by the FRR speakers",
			reportxml.ID("74270"), func() {
				By("Creating a new instance of MetalLB Speakers on workers blocking specific incoming prefixes")
				err := createNewMetalLbDaemonSetAndWaitUntilItsRunningWithAlwaysBlock(tsparams.DefaultTimeout,
					workerLabelMap, []string{externalAdvertisedIPv4Routes[0]})
				Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

				By("Creating external FRR pod on master node")
				frrPod := deployTestPods(addressPool, removePrefixFromIPList(nodeAddrList))

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady(ipv4metalLbIPList[0], "", 64500,
					false, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))

				By("Validating BGP route prefix")
				validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create a frrconfiguration allow all")
				err = createFrrConfiguration("frrconfig-allow-all", ipv4metalLbIPList[0],
					64500, nil, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a FRRconfiguration with allow all")

				frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
					LabelSelector: "app=frr-k8s",
				})
				Expect(err).ToNot(HaveOccurred(), "Fail to find Frrk8 pod list")

				By("Verify that the node FRR pods advertises two routes")
				err = verifyExternalAdvertisedRoutes(frrPod, ipv4NodeAddrList, externalAdvertisedIPv4Routes)
				Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")

				By("Validate BGP received routes")
				Eventually(func() string {
					// Get the routes
					routes, err := frr.VerifyBGPReceivedRoutesOnFrrNodes(frrk8sPods)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")

					// Return the routes to be checked
					return routes
				}, 60*time.Second, 5*time.Second).Should(ContainSubstring(externalAdvertisedIPv4Routes[1]),
					"Fail to find received routes")
			})

		It("Verify the FRR node only receives routes that are configured in the allowed prefixes",
			reportxml.ID("74272"), func() {

				By("Creating a new instance of MetalLB Speakers on workers")
				err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
				Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

				frrPod := deployTestPods(addressPool, removePrefixFromIPList(nodeAddrList))

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady(ipv4metalLbIPList[0], "", 64500, false, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))

				By("Validating BGP route prefix")
				validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create a frrconfiguration with prefix filter")
				err := createFrrConfiguration("frrconfig-filtered", ipv4metalLbIPList[0],
					64500, []string{externalAdvertisedIPv4Routes[0], externalAdvertisedIPv6Routes[0]}, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a FRRconfiguration")

				frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
					LabelSelector: "app=frr-k8s",
				})
				Expect(err).ToNot(HaveOccurred(), "Fail to find Frrk8 pod list")

				By("Verify that the node FRR pods advertises two routes")
				err = verifyExternalAdvertisedRoutes(frrPod, ipv4NodeAddrList, externalAdvertisedIPv4Routes)
				Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")

				By("Validate BGP received routes")
				Eventually(func() string {
					// Get the routes
					routes, err := frr.VerifyBGPReceivedRoutesOnFrrNodes(frrk8sPods)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")

					// Return the routes to be checked
					return routes
				}, 60*time.Second, 5*time.Second).Should(SatisfyAll(
					ContainSubstring(externalAdvertisedIPv4Routes[0]),      // First IP address
					Not(ContainSubstring(externalAdvertisedIPv4Routes[1])), // Second IP address
				), "Fail to find all expected received routes")
			})

		It("Verify that when the allow all mode is configured all routes are received on the FRR speaker",
			reportxml.ID("74273"), func() {

				By("Creating a new instance of MetalLB Speakers on workers")
				err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
				Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

				frrPod := deployTestPods(addressPool, removePrefixFromIPList(nodeAddrList))

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady(ipv4metalLbIPList[0], "", 64500, false, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))

				By("Validating BGP route prefix")
				validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create a frrconfiguration allow all")
				err = createFrrConfiguration("frrconfig-allow-all", ipv4metalLbIPList[0], 64500, nil, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a FRRconfiguration with allow all")

				frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
					LabelSelector: "app=frr-k8s",
				})
				Expect(err).ToNot(HaveOccurred(), "Fail to find Frrk8 pod list")

				By("Verify that the node FRR pods advertises two routes")
				err = verifyExternalAdvertisedRoutes(frrPod, ipv4NodeAddrList, externalAdvertisedIPv4Routes)
				Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")

				By("Validate BGP received routes")
				Eventually(func() string {
					// Get the routes
					routes, err := frr.VerifyBGPReceivedRoutesOnFrrNodes(frrk8sPods)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")

					// Return the routes to be checked
					return routes
				}, 60*time.Second, 5*time.Second).Should(SatisfyAll(
					ContainSubstring(externalAdvertisedIPv4Routes[0]), // First IP address
					ContainSubstring(externalAdvertisedIPv4Routes[1]), // Second IP address
				), "Fail to find all expected received routes")
			})

		It("Verify that a FRR speaker can be updated by merging two different FRRConfigurations",
			reportxml.ID("74274"), func() {

				By("Creating a new instance of MetalLB Speakers on workers")
				err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
				Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

				frrPod := deployTestPods(addressPool, removePrefixFromIPList(nodeAddrList))

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady(ipv4metalLbIPList[0], "", 64500, false, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))

				By("Validating BGP route prefix")
				validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create first frrconfiguration that receieves a single route")
				err := createFrrConfiguration("frrconfig-filtered-1", ipv4metalLbIPList[0], 64500,
					[]string{externalAdvertisedIPv4Routes[0], externalAdvertisedIPv6Routes[0]}, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a first FRRconfiguration")

				frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
					LabelSelector: "app=frr-k8s",
				})
				Expect(err).ToNot(HaveOccurred(), "Fail to find Frrk8 pod list")

				By("Verify that the node FRR pods advertises two routes")
				err = verifyExternalAdvertisedRoutes(frrPod, ipv4NodeAddrList, externalAdvertisedIPv4Routes)
				Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")

				By("Validate BGP received only the first route")
				Eventually(func() string {
					// Get the routes
					routes, err := frr.VerifyBGPReceivedRoutesOnFrrNodes(frrk8sPods)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")

					// Return the routes to be checked
					return routes
				}, 60*time.Second, 5*time.Second).Should(SatisfyAll(
					ContainSubstring(externalAdvertisedIPv4Routes[0]),      // First IP address
					Not(ContainSubstring(externalAdvertisedIPv4Routes[1])), // Second IP address
				), "Fail to find all expected received routes")

				By("Create second frrconfiguration that receives a single route")
				err = createFrrConfiguration("frrconfig-filtered-2", ipv4metalLbIPList[0], 64500,
					[]string{externalAdvertisedIPv4Routes[1], externalAdvertisedIPv6Routes[1]}, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a second FRRconfiguration")

				By("Validate BGP received only the first route")
				Eventually(func() string {
					// Get the routes
					routes, err := frr.VerifyBGPReceivedRoutesOnFrrNodes(frrk8sPods)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")

					// Return the routes to be checked
					return routes
				}, 60*time.Second, 5*time.Second).Should(SatisfyAll(
					ContainSubstring(externalAdvertisedIPv4Routes[0]), // First IP address
					ContainSubstring(externalAdvertisedIPv4Routes[1]), // Second IP address
				), "Fail to find all expected received routes")
			})

		It("Verify that a FRR speaker rejects a contrasting FRRConfiguration merge",
			reportxml.ID("74275"), func() {

				By("Creating a new instance of MetalLB Speakers on workers")
				err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
				Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

				frrPod := deployTestPods(addressPool, removePrefixFromIPList(nodeAddrList))

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady(ipv4metalLbIPList[0], "", 64500,
					false, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))

				By("Validating BGP route prefix")
				validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create first frrconfiguration that receieves a single route")
				err := createFrrConfiguration("frrconfig-filtered-1", ipv4metalLbIPList[0], 64500,
					[]string{externalAdvertisedIPv4Routes[0], externalAdvertisedIPv6Routes[0]}, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a first FRRconfiguration")

				frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
					LabelSelector: "app=frr-k8s",
				})
				Expect(err).ToNot(HaveOccurred(), "Fail to find Frrk8 pod list")

				By("Verify that the node FRR pods advertises two routes")
				err = verifyExternalAdvertisedRoutes(frrPod, ipv4NodeAddrList, externalAdvertisedIPv4Routes)
				Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")

				By("Validate BGP received routes")
				Eventually(func() string {
					// Get the routes
					routes, err := frr.VerifyBGPReceivedRoutesOnFrrNodes(frrk8sPods)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")

					// Return the routes to be checked
					return routes
				}, 60*time.Second, 5*time.Second).Should(SatisfyAll(
					ContainSubstring(externalAdvertisedIPv4Routes[0]),      // First IP address
					Not(ContainSubstring(externalAdvertisedIPv4Routes[1])), // Second IP address
				), "Fail to find all expected received routes")

				By("Create second frrconfiguration with an incorrect AS configuration")
				err = createFrrConfiguration("frrconfig-filtered-2", ipv4metalLbIPList[0], 64501,
					[]string{externalAdvertisedIPv4Routes[1], externalAdvertisedIPv6Routes[1]}, false)
				Expect(err).To(HaveOccurred(), "successful created the FRRconfiguration and expected to fail")
			})
	})

	Context("BGP Multihop", func() {

		BeforeAll(func() {

			var (
				workerNodeList []*nodes.Builder
			)

			workerNodeList, err := nodes.List(APIClient,
				metav1.ListOptions{LabelSelector: labels.Set(NetConfig.WorkerLabelMap).String()})
			Expect(err).ToNot(HaveOccurred(), "Failed to discover worker nodes")

			By("Creating a new instance of MetalLB Speakers on workers")
			err = metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
			Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

			_, err = pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
				LabelSelector: "app=frr-k8s",
			})
			Expect(err).ToNot(HaveOccurred(), "Fail to list speaker pods")

			By("Collecting information before test")

			Expect(err).ToNot(HaveOccurred(), "Failed to list speaker pods")
			By("Setting test iteration parameters")
			_, _, _, _, addressPool, _, err :=
				metallbenv.DefineIterationParams(
					ipv4metalLbIPList, ipv6metalLbIPList, ipv4NodeAddrList, ipv6NodeAddrList, "IPv4")
			Expect(err).ToNot(HaveOccurred(), "Fail to set iteration parameters")

			By("Collecting SR-IOV interfaces for qinq testing")
			srIovInterfacesUnderTest, err := NetConfig.GetSriovInterfaces(1)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve SR-IOV interfaces for testing")

			fmt.Println("SRIOV INTERFACE", srIovInterfacesUnderTest[0])

			By("Creating an IPAddressPool and BGPAdvertisement")
			ipAddressPool := setupBgpAdvertisement(addressPool, int32(32))

			By("Creating a MetalLB service")
			setupMetalLbService("IPv4", ipAddressPool, "Cluster")

			By("Creating nginx test pod on worker node")
			setupNGNXPod(workerNodeList[0].Definition.Name)
		})

		AfterEach(func() {

			By("Removing static routes from the speakers")
			frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
				LabelSelector: tsparams.FRRK8sDefaultLabel,
			})
			Expect(err).ToNot(HaveOccurred(), "Failed to list pods")

			speakerRoutesMap := buildRoutesMapWithSpecificRoutes(frrk8sPods, []string{"10.100.100.131", "10.100.100.132"})
			Expect(err).ToNot(HaveOccurred(), "Failed to build speaker route map")

			for _, frrk8sPod := range frrk8sPods {
				out, err := frr.SetStaticRoute(frrk8sPod, "del", "172.16.0.1", speakerRoutesMap)
				Expect(err).ToNot(HaveOccurred(), out)
			}

			srIovInterfacesUnderTest, err := NetConfig.GetSriovInterfaces(1)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve SR-IOV interfaces for testing")

			vlanID, err := NetConfig.GetVLAN()
			Expect(err).ToNot(HaveOccurred(), "Fail to set vlanID")

			By("Removing secondary interface on worker-0")
			secIntWorker0Policy := nmstate.NewPolicyBuilder(APIClient, "sec-int-worker0", NetConfig.WorkerLabelMap).
				WithAbsentInterface(fmt.Sprintf("%s.%d", srIovInterfacesUnderTest[0], vlanID))
			err = netnmstate.UpdatePolicyAndWaitUntilItsAvailable(netparam.DefaultTimeout, secIntWorker0Policy)
			Expect(err).ToNot(HaveOccurred(), "Failed to update NMState network policy")

			By("Removing secondary interface on worker-1")
			secIntWorker1Policy := nmstate.NewPolicyBuilder(APIClient, "sec-int-worker1", NetConfig.WorkerLabelMap).
				WithAbsentInterface(fmt.Sprintf("%s.%d", srIovInterfacesUnderTest[0], vlanID))
			err = netnmstate.UpdatePolicyAndWaitUntilItsAvailable(netparam.DefaultTimeout, secIntWorker1Policy)
			Expect(err).ToNot(HaveOccurred(), "Failed to update NMState network policy")

			By("Removing secondary interfaces")
			secIntWorkers, err := nmstate.ListPolicy(APIClient)
			Expect(err).ToNot(HaveOccurred(), "Failed to list pods")

			for _, secInt := range secIntWorkers {
				fmt.Println(secInt.Definition.Name)
				_, err = secInt.Delete()
				Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Failed to delete secondary interface: %s", secInt))
			}

			resetOperatorAndTestNS()
			//
			//By("Removing NMState policies")
			//err = nmstate.CleanAllNMStatePolicies(APIClient)
			//Expect(err).ToNot(HaveOccurred(), "Failed to remove all NMState policies")
			//})

		})

		FIt("Validate a FRR node receives and sends IPv4 and IPv6 routes from an IBGP multihop FRR instance",
			reportxml.ID("74278"), func() {

				By("Collecting information before test")
				frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
					LabelSelector: "app=frr-k8s",
				})
				Expect(err).ToNot(HaveOccurred(), "Failed to list speaker pods")
				By("Setting test iteration parameters")
				masterClientPodIP, _, _, nodeAddrList, addressPool, _, err :=
					metallbenv.DefineIterationParams(
						ipv4metalLbIPList, ipv6metalLbIPList, ipv4NodeAddrList, ipv6NodeAddrList, "IPv4")
				Expect(err).ToNot(HaveOccurred(), "Fail to set iteration parameters")

				By("Adding static routes to the speakers")
				speakerRoutesMap := buildRoutesMapWithSpecificRoutes(frrk8sPods, ipv4metalLbIPList)

				for _, frrk8sPod := range frrk8sPods {
					out, err := frr.SetStaticRoute(frrk8sPod, "add", masterClientPodIP, speakerRoutesMap)
					Expect(err).ToNot(HaveOccurred(), out)
				}

				hubFrrWorker0 := createStaticIPAnnotations(tsparams.ExternalMacVlanNADName, tsparams.HubMacVlanNADName, []string{ipv4NodeAddrList[0]},
					[]string{hubIPaddresses[0]})

				hubFrrWorker1 := createStaticIPAnnotations(tsparams.ExternalMacVlanNADName, tsparams.HubMacVlanNADName, []string{ipv4NodeAddrList[1]},
					[]string{hubIPaddresses[1]})

				By("Creating MetalLb Hub pod configMap")
				hubConfigMap := createHubConfigMap("frr-hub-node-config")

				By("Creating FRR Hub Worker-0 Pod")
				_ = createFrrHubPod("hub-pod-worker-0",
					workerNodeList[0].Object.Name, hubConfigMap.Definition.Name, []string{}, hubFrrWorker0)

				By("Creating FRR Hub Worker-1 Pod")
				_ = createFrrHubPod("hub-pod-worker-1",
					workerNodeList[1].Object.Name, hubConfigMap.Definition.Name, []string{}, hubFrrWorker1)

				By("Define and create an external frr pod on Master0")
				frrPod := defineAndCreateMasterFrrPod(64500, nodeAddrList, false)

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady("172.16.0.1", "", tsparams.LocalBGPASN,
					false, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))

				By("Validating BGP route prefix")
				validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create a frrconfiguration allow all for EBGP multihop")
				err = createFrrConfiguration("frrconfig-allow-all", "172.16.0.1", 64500, nil, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create an allow-all FRRconfiguration")

				By("Verify that the node FRR pods advertises two routes")
				err = verifyExternalAdvertisedRoutes(frrPod, ipv4NodeAddrList, externalAdvertisedIPv4Routes)
				Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")

				By("Validate BGP received routes")
				Eventually(func() string {
					// Get the routes
					routes, err := frr.VerifyBGPReceivedRoutesOnFrrNodes(frrk8sPods)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")

					// Return the routes to be checked
					return routes
				}, 60*time.Second, 5*time.Second).Should(SatisfyAll(
					ContainSubstring(externalAdvertisedIPv4Routes[0]), // First IP address
					ContainSubstring(externalAdvertisedIPv4Routes[1]), // Second IP address
				), "Fail to find all expected received routes")
			})

		It("Validate a FRR node receives and sends IPv4 and IPv6 routes from an EBGP multihop FRR instance",
			reportxml.ID("47279"), func() {

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
				frrPod := defineAndCreateMasterFrrPod(64501, nodeAddrList, true)

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady("172.16.0.1", "", tsparams.RemoteBGPASN,
					true, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))

				By("Validating BGP route prefix")
				validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create a frrconfiguration allow all for EBGP multihop")
				err = createFrrConfiguration("frrconfig-allow-all", "172.16.0.1", 64501, nil, true)
				Expect(err).ToNot(HaveOccurred(), "Fail to create an allow-all FRRconfiguration")

				By("Verify that the node FRR pods advertises two routes")
				err = verifyExternalAdvertisedRoutes(frrPod, ipv4NodeAddrList, externalAdvertisedIPv4Routes)
				Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")

				By("Validate BGP received routes")
				Eventually(func() string {
					// Get the routes
					routes, err := frr.VerifyBGPReceivedRoutesOnFrrNodes(frrk8sPods)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")

					// Return the routes to be checked
					return routes
				}, 60*time.Second, 5*time.Second).Should(SatisfyAll(
					ContainSubstring(externalAdvertisedIPv4Routes[0]), // First IP address
					ContainSubstring(externalAdvertisedIPv4Routes[1]), // Second IP address
				), "Fail to find all expected received routes")
			})

		It("Verify Frrk8 iBGP multihop over a secondary interface",
			reportxml.ID("75248"), func() {

				srIovInterfacesUnderTest, err := NetConfig.GetSriovInterfaces(1)
				Expect(err).ToNot(HaveOccurred(), "Failed to retrieve SR-IOV interfaces for testing")

				vlanID, err := NetConfig.GetVLAN()
				Expect(err).ToNot(HaveOccurred(), "Fail to set vlanID")

				fmt.Println("VLANID", vlanID)

				fmt.Println("SRIOV INTERFACE", srIovInterfacesUnderTest[0])

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

				By("create a secondary IP address on the worker node 0")
				err = createSecondaryInterfaceOnNode("sec-int-worker0", "worker-0",
					srIovInterfacesUnderTest[0], "10.100.100.254", "2001:100::254", vlanID)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a secondary interface on worker0")

				By("create a secondary IP address on the worker node 1")
				err = createSecondaryInterfaceOnNode("sec-int-worker1", "worker-1",
					srIovInterfacesUnderTest[0], "10.100.100.253", "2001:100::253", vlanID)
				Expect(err).ToNot(HaveOccurred(), "Fail to create a secondary interface on worker1")

				By("Adding static routes to the speakers")
				speakerRoutesMap := buildRoutesMapWithSpecificRoutes(frrk8sPods, []string{"10.100.100.131", "10.100.100.132"})
				Expect(err).ToNot(HaveOccurred(), "Failed to build speaker route map")

				for _, frrk8sPod := range frrk8sPods {
					// Wait until the interface is created before adding the static route
					Eventually(func() error {
						// Here you can add logic to check if the interface exists
						out, err := frr.SetStaticRoute(frrk8sPod, "add", "172.16.0.1/32", speakerRoutesMap)
						if err != nil {
							return fmt.Errorf("error adding static route: %s", out)
						}
						return nil
					}, time.Minute, 5*time.Second).Should(Succeed(), "Failed to add static route for pod %s", frrk8sPod.Definition.Name)
				}

				//vlanID := uint16(177)
				interfaceNameWithVlan := fmt.Sprintf("%s.%d", srIovInterfacesUnderTest[0], vlanID)
				fmt.Println("interfaceNameWithVlan", interfaceNameWithVlan)

				By("Creating External NAD for hub FRR pods secondary interface")
				createExternalNadWithMasterInterface(tsparams.HubMacVlanNADDecIntName, interfaceNameWithVlan)

				//createExternalNadWithMasterInterface(tsparams.HubMacVlanNADDecIntName, "ens8f0np0.177")

				//hubFrrWorker0Config := defineHubFrrPod(tsparams.HubMacVlanNADDecIntName, tsparams.HubMacVlanNADName,
				//	[]string{"10.100.100.131/24", "2001:81:81::1/64"}, []string{"172.16.0.10/24", "2001:100:100::10/64"},
				//	hub0BRStaticSecIntIPAnnotation, "ens8f0np0.177")

				//By("Creating External NAD for hub FRR pods secondary interface")
				//createExternalNadWithMasterInterface(tsparams.HubMacVlanNADDecIntName, "ens8f0np0.177")

				By("Creating External NAD for master FRR pod")
				createExternalNad(tsparams.ExternalMacVlanNADName)

				By("Creating External NAD for hub FRR pods")
				createExternalNad(tsparams.HubMacVlanNADName)

				By("Creating MetalLb Hub pod configMap")
				createHubConfigMapSecInt := createHubConfigMap("frr-hub-node-config")

				By("Creating static ip annotation for hub0")
				hub0BRStaticSecIntIPAnnotation := pod.StaticIPAnnotation(tsparams.HubMacVlanNADDecIntName,
					[]string{"10.100.100.131/24", "2001:81:81::131/64"})
				hub0BRStaticSecIntIPAnnotation = append(hub0BRStaticSecIntIPAnnotation,
					pod.StaticIPAnnotation(tsparams.HubMacVlanNADName, []string{"172.16.0.10/24", "2001:100:100::10/64"})...)

				By("Creating static ip annotation for hub1")
				hub1BRstaticSecIntIPAnnotation := pod.StaticIPAnnotation(tsparams.HubMacVlanNADDecIntName,
					[]string{"10.100.100.132/24", "2001:81:81::132/64"})
				hub1BRstaticSecIntIPAnnotation = append(hub1BRstaticSecIntIPAnnotation,
					pod.StaticIPAnnotation(tsparams.HubMacVlanNADName, []string{"172.16.0.11/24", "2001:100:100::11/64"})...)

				By("Creating FRR Hub Worker-0 Pod")
				_ = createFrrHubPod("hub-pod-worker-0",
					workerNodeList[0].Object.Name, createHubConfigMapSecInt.Definition.Name, []string{}, hub0BRStaticSecIntIPAnnotation)

				By("Creating FRR Hub Worker-1 Pod")
				_ = createFrrHubPod("hub-pod-worker-1",
					workerNodeList[1].Object.Name, createHubConfigMapSecInt.Definition.Name, []string{}, hub1BRstaticSecIntIPAnnotation)

				By("Creating configmap and MetalLb Master pod")
				frrPod := defineAndCreateMasterFrrPod(64500, []string{"10.100.100.254", "10.100.100.253"}, false)

				By("Creating BGP Peers")
				createBGPPeerAndVerifyIfItsReady("172.16.0.1", "", tsparams.LocalBGPASN,
					false, frrk8sPods)

				By("Checking that BGP session is established and up")
				verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, []string{"10.100.100.254", "10.100.100.253"})

				By("Validating BGP route prefix")
				validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)

				By("Create a frrconfiguration allow all for IBGP multihop")
				err = createFrrConfiguration("frrconfig-allow-all", "172.16.0.1", 64500, nil, false)
				Expect(err).ToNot(HaveOccurred(), "Fail to create an allow-all FRRconfiguration")

				By("Verify that the node FRR pods advertises two routes")
				err = verifyExternalAdvertisedRoutes(frrPod, []string{"10.100.100.254", "10.100.100.253"}, externalAdvertisedIPv4Routes)
				Expect(err).ToNot(HaveOccurred(), "Fail to find advertised routes")

				By("Validate BGP received routes")
				Eventually(func() string {
					// Get the routes
					routes, err := frr.VerifyBGPReceivedRoutesOnFrrNodes(frrk8sPods)
					Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP routes")

					// Return the routes to be checked
					return routes
				}, 60*time.Second, 5*time.Second).Should(SatisfyAll(
					ContainSubstring(externalAdvertisedIPv4Routes[0]), // First IP address
					ContainSubstring(externalAdvertisedIPv4Routes[1]), // Second IP address
				), "Fail to find all expected received routes")
			})

	})

})

func createNewMetalLbDaemonSetAndWaitUntilItsRunningWithAlwaysBlock(timeout time.Duration,
	nodeLabel map[string]string, prefixes []string) error {
	By("Verifying if metalLb daemonset is running")

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

	metalLbIo.WithFRRConfigAlwaysBlock(prefixes)
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

func deployTestPods(addressPool []string, nodeBGPPeerIPs []string) *pod.Builder {

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

	masterConfigMap := createConfigMapWithStaticRoutes(tsparams.LocalBGPASN, nodeBGPPeerIPs, false, false)

	By("Creating FRR Pod")

	frrPod := createFrrPod(
		masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, staticIPAnnotation)

	return frrPod
}

func createFrrConfiguration(name, bgpPeerIP string, remoteAS uint32,
	filteredIP []string, ebgp bool) error {
	frrConfig := metallb.NewFrrConfigurationBuilder(APIClient, name,
		NetConfig.MlbOperatorNamespace).WithBGPRouter(tsparams.LocalBGPASN).
		WithBGPNeighbor(bgpPeerIP, remoteAS, 0)
	if len(filteredIP) > 0 {
		frrConfig.WithToReceiveModeFiltered(filteredIP, 0, 0)
	} else {
		frrConfig.WithToReceiveModeAll(0, 0)
	}

	if ebgp {
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

		return err
	}

	return nil
}

func defineAndCreateMasterFrrPod(localAS int, ipv4NodeAddrList []string, ebgpMultiHop bool) *pod.Builder {
	masterConfigMap := createConfigMapWithStaticRoutes(localAS, ipv4NodeAddrList, ebgpMultiHop, false)

	By("Creating static ip annotation for master FRR pod")

	masterStaticIPAnnotation := pod.StaticIPAnnotation(
		tsparams.HubMacVlanNADName, []string{"172.16.0.1/24", "2001:100:100::254/64"})

	By("Creating FRR Master Pod")

	frrPod := createFrrPod(
		masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, masterStaticIPAnnotation)

	return frrPod
}

func createConfigMapWithStaticRoutes(
	bgpAsn int, bgpPeerIPList []string, enableMultiHop, enableBFD bool) *configmap.Builder {
	frrBFDConfig := frr.DefineBGPConfigWithStaticRouteAndNetwork(
		bgpAsn, tsparams.LocalBGPASN, hubIPaddresses, externalAdvertisedIPv4Routes,
		externalAdvertisedIPv6Routes, removePrefixFromIPList(bgpPeerIPList), enableMultiHop, enableBFD)
	configMapData := frr.DefineBaseConfig(tsparams.DaemonsFile, frrBFDConfig, "")
	masterConfigMap, err := configmap.NewBuilder(APIClient, "frr-master-node-config", tsparams.TestNamespaceName).
		WithData(configMapData).Create()
	Expect(err).ToNot(HaveOccurred(), "Failed to create config map")

	return masterConfigMap
}

func verifyExternalAdvertisedRoutes(frrPod *pod.Builder, ipv4NodeAddrList, externalExpectedRoutes []string) error {
	advertisedRoutes, err := frr.GetBGPAdvertisedRoutes(frrPod, removePrefixFromIPList(ipv4NodeAddrList))
	if err != nil {
		return fmt.Errorf("failed to find advertised routes: %w", err)
	}

	actualRoutes := strings.Split(strings.TrimSpace(advertisedRoutes), "\n")
	expectedRoutes := externalExpectedRoutes

	// Check that the actual routes contain all the expected routes
	for _, expectedRoute := range expectedRoutes {
		matched, err := ContainElement(expectedRoute).Match(actualRoutes)
		if err != nil {
			return fmt.Errorf("failed to match route %s: %w", expectedRoute, err)
		}

		if !matched {
			return fmt.Errorf("expected route %s not found", expectedRoute)
		}
	}

	// Return nil if all expected routes are found
	return nil
}

func resetOperatorAndTestNS() {
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
	Expect(err).ToNot(HaveOccurred(), "Failed to clean test namespace")
}

func buildRoutesMapWithSpecificRoutes(podList []*pod.Builder, nextHopList []string) map[string]string {
	Expect(len(podList)).ToNot(BeZero(), "Pod list is empty")
	Expect(len(nextHopList)).ToNot(BeZero(), "Nexthop IP addresses list is empty")
	Expect(len(nextHopList)).To(BeNumerically(">=", len(podList)),
		fmt.Sprintf("Number of speaker IP addresses[%d] is less than the number of pods[%d]",
			len(nextHopList), len(podList)))

	routesMap := make(map[string]string)

	for _, frrPod := range podList {
		if frrPod.Definition.Spec.NodeName == workerNodeList[0].Definition.Name {
			routesMap[frrPod.Definition.Spec.NodeName] = nextHopList[1]
		} else {
			routesMap[frrPod.Definition.Spec.NodeName] = nextHopList[0]
		}
	}

	return routesMap
}

//func defineHubFrrPod(nameNadInside, nameNadOutside string, nodeSideIPs, externalSideIPs []string, nameIPAnnotation string, secondaryInterface ...string) *configmap.Builder {
//	var hubConfigMap *configmap.Builder
//	var nameIPAnnotation []*types.NetworkSelectionElement(nameIPAnnotation)
//
//	if len(secondaryInterface) > 0 {
//		By("Creating External NAD for hub FRR pods secondary interface")
//		createExternalNadWithMasterInterface(tsparams.HubMacVlanNADDecIntName, secondaryInterface[0])
//	}
//
//	By("Creating External NAD for master FRR pod")
//	createExternalNad(tsparams.ExternalMacVlanNADName)
//
//	By("Creating MetalLb Hub pod configMap")
//	hubConfigMap = createHubConfigMap("frr-hub-node-config")
//
//
//	return hubConfigMap
//}

func createSecondaryInterfaceOnNode(policyName, nodeName, interfaceName, ipv4Address, ipv6Address string, vlanID uint16) error {
	secondaryInterface := nmstate.NewPolicyBuilder(APIClient, policyName, map[string]string{
		"kubernetes.io/hostname": nodeName,
	})

	secondaryInterface.WithVlanInterfaceIP(interfaceName, vlanID, ipv4Address, ipv6Address)

	_, err := secondaryInterface.Create()

	if err != nil {
		return fmt.Errorf("fail to create secondary interface: %s.+%d", interfaceName, vlanID)
	}

	return nil
}

func createStaticIPAnnotations(internalNADName, externalNADName string, internalIPAddresses, externalIPAddresses []string) []*types.NetworkSelectionElement {
	ipAnnotation := pod.StaticIPAnnotation(internalNADName, internalIPAddresses)
	ipAnnotation = append(ipAnnotation,
		pod.StaticIPAnnotation(externalNADName, externalIPAddresses)...)

	return ipAnnotation
}
