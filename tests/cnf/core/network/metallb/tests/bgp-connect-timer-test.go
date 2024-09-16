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
	"time"
)

var _ = Describe("FRR", Ordered, Label(tsparams.LabelBGPTestCases), ContinueOnFailure, func() {

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

	BeforeEach(func() {
		By("Creating a new instance of MetalLB Speakers on workers")
		err := metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
		Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

		_, err = pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
			LabelSelector: tsparams.FRRK8sDefaultLabel,
		})
		Expect(err).ToNot(HaveOccurred(), "Fail to list speaker pods")

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
			metallb.GetMetalLbIoGVR())
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

	It("Verify configuration of a FRR node router peer with the connectTime less than the default of 120 seconds",
		reportxml.ID("74414"), func() {

			By("Collecting information before test")
			frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
				LabelSelector: tsparams.FRRK8sDefaultLabel,
			})
			Expect(err).ToNot(HaveOccurred(), "Failed to list speaker pods")

			By("Creating BGP Peers with 10 second retry connect timer")
			createBGPPeerWithConnectTimeAndVerifyIfItsReady(metav1.Duration{Duration: 10 * time.Second},
				ipv4metalLbIPList[0], "", 64500, false, frrk8sPods)

			By("Validate BGP Peers with 10 second retry connect timer")
			frrk8sPods, err = pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
				LabelSelector: "app=frr-k8s",
			})
			Expect(err).ToNot(HaveOccurred(), "Fail to find Frrk8 pod list")

			By("Verify BGP connect timer for bgppeer")
			Eventually(func() string {
				// Get the routes
				connectTime, err := frr.VerifyBGPConnectTime(frrk8sPods, "10.46.81.131", 10)
				Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP connect time")

				// Return the routes to be checked
				return connectTime
			}, 60*time.Second, 5*time.Second).Should(ContainSubstring("ConnectRetryTimer: 10"),
				"Fail to find received BGP connect time")
		})

	FIt("Verify the retry timers connect on an established Metallb neighbor with a timer connect less then the default of 120",
		reportxml.ID("74416"), func() {

			By("Collecting information before test")
			frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
				LabelSelector: tsparams.FRRK8sDefaultLabel,
			})
			Expect(err).ToNot(HaveOccurred(), "Failed to list speaker pods")

			By("Create an external FRR Pod")
			frrPod := createAndDeployFRRPod()

			By("Creating BGP Peers with 10 second retry connect timer")
			createBGPPeerWithConnectTimeAndVerifyIfItsReady(metav1.Duration{Duration: 10 * time.Second},
				ipv4metalLbIPList[0], "", 64500, false, frrk8sPods)

			By("Validate BGP Peers with 10 second retry connect timer")
			frrk8sPods, err = pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
				LabelSelector: "app=frr-k8s",
			})
			Expect(err).ToNot(HaveOccurred(), "Fail to find Frrk8 pod list")

			By("Verify BGP connect timer for bgppeer")
			Eventually(func() string {
				// Get the routes
				connectTime, err := frr.VerifyBGPConnectTime(frrk8sPods, "10.46.81.131", 10)
				Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP connect time")

				// Return the routes to be checked
				return connectTime
			}, 60*time.Second, 5*time.Second).Should(ContainSubstring("ConnectRetryTimer: 10"),
				"Fail to find received BGP connect time")

			By("Reset the BGP session ")
			err = frr.ResetBGPConnection(frrPod)
			Expect(err).ToNot(HaveOccurred(), "Failed to reset BGP connection")

			By("Verify that BGP session is established and up in less then 10 seconds")
			verifyMetalLbBGPSessionsAreUPOnFrrNode10Seconds(frrPod, removePrefixFromIPList(ipv4NodeAddrList))
		})

	It("Update the timer to less then the default on an existing BGP connection",
		reportxml.ID("74417"), func() {

			By("Collecting information before test")
			frrk8sPods, err := pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
				LabelSelector: tsparams.FRRK8sDefaultLabel,
			})
			Expect(err).ToNot(HaveOccurred(), "Failed to list speaker pods")

			By("Creating BGP Peers")
			createBGPPeerAndVerifyIfItsReady(ipv4metalLbIPList[0], "", 64500, false, frrk8sPods)

			By("Validate BGP Peers with the default retry connect timer")
			frrk8sPods, err = pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
				LabelSelector: "app=frr-k8s",
			})
			Expect(err).ToNot(HaveOccurred(), "Fail to find Frrk8 pod list")

			Eventually(func() string {
				// Get the routes
				connectTime, err := frr.VerifyBGPConnectTime(frrk8sPods, "10.46.81.131", 120)
				Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP connect time")

				// Return the routes to be checked
				return connectTime
			}, 60*time.Second, 5*time.Second).Should(ContainSubstring("ConnectRetryTimer: 120"),
				"Fail to find received BGP connect time")

			By("Update the BGP Peers connect timer to 10 seconds")
			bgpPeer, err := metallb.PullBGPPeer(APIClient, "testpeer", NetConfig.MlbOperatorNamespace)
			Expect(err).ToNot(HaveOccurred(), "Failed to find bgp peer")

			_, err = bgpPeer.WithConnectTimer(metav1.Duration{Duration: 10 * time.Second}).Update(true)

			By("Validate BGP Peers with the default retry connect timer")
			Eventually(func() string {
				// Get the routes
				connectTime, err := frr.VerifyBGPConnectTime(frrk8sPods, "10.46.81.131", 10)
				Expect(err).ToNot(HaveOccurred(), "Failed to verify BGP connect time")

				// Return the routes to be checked
				return connectTime
			}, 60*time.Second, 5*time.Second).Should(ContainSubstring("ConnectRetryTimer: 10"),
				"Fail to find received BGP connect time")
		})
})

func createAndDeployFRRPod() *pod.Builder {

	var err error

	By("Creating External NAD")
	createExternalNad()

	By("Creating static ip annotation")
	staticIPAnnotation := pod.StaticIPAnnotation(
		externalNad.Definition.Name, []string{fmt.Sprintf("%s/%s", ipv4metalLbIPList[0], "24")})

	By("Creating MetalLb configMap")
	masterConfigMap := createConfigMap(tsparams.LocalBGPASN, ipv4NodeAddrList, false, false)

	By("Creating FRR Pod")
	frrPod := createFrrPod(
		masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, staticIPAnnotation)

	time.Sleep(20 * time.Second)
	Eventually(func() error {
		return err
	}, time.Minute, tsparams.DefaultRetryInterval).ShouldNot(HaveOccurred(),
		"Failed to collect metrics from speaker pods")

	return frrPod
}
