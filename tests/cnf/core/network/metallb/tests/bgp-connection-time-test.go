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
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/metallbenv"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/metallb/internal/tsparams"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

	BeforeEach(func() {
		By("Creating a new instance of MetalLB Speakers on workers")
		err := metallbenv.CreateNewMetalLbDaemonSetAndWaitUntilItsRunning(tsparams.DefaultTimeout, workerLabelMap)
		Expect(err).ToNot(HaveOccurred(), "Failed to recreate metalLb daemonset")

		_, err = pod.List(APIClient, NetConfig.MlbOperatorNamespace, metav1.ListOptions{
			LabelSelector: tsparams.FRRK8sDefaultLabel,
		})
		Expect(err).ToNot(HaveOccurred(), "Fail to list speaker pods")

		By("Creating External NAD")
		createExternalNad(tsparams.ExternalMacVlanNADName)

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

		By("Creating static ip annotation")
		staticIPAnnotation := pod.StaticIPAnnotation(
			externalNad.Definition.Name, []string{fmt.Sprintf("%s/%s", ipv4metalLbIPList[0], "24")})

		By("Creating MetalLb configMap")
		masterConfigMap := createConfigMap(tsparams.LocalBGPASN, ipv4NodeAddrList, false, false)

		By("Creating FRR Pod")
		frrPod := createFrrPod(
			masterNodeList[0].Object.Name, masterConfigMap.Definition.Name, []string{}, staticIPAnnotation)

		By("Creating BGP Peers")
		createBGPPeerAndVerifyIfItsReady(ipv4metalLbIPList[0], "", 64500, false, frrk8sPods)

		By("Checking that BGP session is established and up")
		verifyMetalLbBGPSessionsAreUPOnFrrPod(frrPod, removePrefixFromIPList(ipv4NodeAddrList))
		time.Sleep(30 * time.Second)
		By("Validating BGP route prefix")
		validatePrefix(frrPod, "IPv4", removePrefixFromIPList(nodeAddrList), addressPool, 32)
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

	It("Verify configuration of a FRR node router peer with the connectTime less than the default of 120 seconds",
		reportxml.ID("74414"), func() {
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
})
