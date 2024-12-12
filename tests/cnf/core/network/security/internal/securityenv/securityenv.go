package securityenv

import (
	"fmt"
	"strings"
	"time"

	butaneConfig "github.com/coreos/butane/config"
	"github.com/coreos/butane/config/common"
	"github.com/golang/glog"
	"github.com/openshift-kni/eco-goinfra/pkg/deployment"
	"github.com/openshift-kni/eco-goinfra/pkg/namespace"
	"github.com/openshift-kni/eco-goinfra/pkg/nodes"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	"github.com/openshift-kni/eco-gotests/tests/cnf/core/network/security/internal/tsparams"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const butaneVersion = "4.17.0"

func CreateMC(nodeRole string) (machineConfig []byte, err error) {
	butaneConfigVar := createButaneConfig(nodeRole)
	options := common.TranslateBytesOptions{}
	machineConfig, _, err = butaneConfig.TranslateBytes(butaneConfigVar, options)
	if err != nil {
		return nil, fmt.Errorf("failed to covert the ButaneConfig to yaml %v: ", err)
	}
	//nodeName := "worker-0"

	//// Ensure the artifacts directory exists
	//if _, err := os.Stat(artifactsDir); os.IsNotExist(err) {
	//	if err := os.MkdirAll(artifactsDir, 0755); err != nil {
	//		Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("failed to create artifacts directory: %s", artifactsDir))
	//	}
	//}
	//// Write the machine config to a file
	//filePath := filepath.Join(artifactsDir, "nftables-after-reboot-"+nodeName)
	//if err := os.WriteFile(filePath, machineConfig, 0644); err != nil {
	//	return nil, fmt.Errorf("failed to write machine config to file: %w", err)
	//}

	fmt.Println("machineConfig", string(machineConfig))

	return machineConfig, nil
}

func createButaneConfig(nodeRole string) []byte {
	butaneConfig := fmt.Sprintf(`variant: openshift
version: %s
metadata:
  labels:
    machineconfiguration.openshift.io/role: worker
  name: "98-nftables-cnf-worker"
systemd:
  units:
    - name: "nftables.service"
      enabled: true
      contents: |
        [Unit]
        Description=Netfilter Tables
        Documentation=man:nft(8)
        Wants=network-pre.target
        Before=network-pre.target
        [Service]
        Type=oneshot
        ProtectSystem=full
        ProtectHome=true
        ExecStart=/sbin/nft -f /etc/sysconfig/nftables.conf
        ExecReload=/sbin/nft -f /etc/sysconfig/nftables.conf
        ExecStop=/sbin/nft 'add table inet openshift_filter; delete table inet openshift_filter'
        RemainAfterExit=yes
        [Install]
        WantedBy=multi-user.target
storage:
  files:
    - path: /etc/sysconfig/nftables.conf
      mode: 0600
      overwrite: true
      contents:
        inline: |
          table inet custom_table
          delete table inet custom_table
          table inet custom_table {
          	chain custom_chain_INPUT {
          		type filter hook input priority 1; policy accept;
          		# Drop TCP port 8888 and log
          		tcp dport 8888 log prefix "[USERFIREWALL] PACKET DROP: " drop
          	}
          }`, "4.17.0")
	butaneConfig = strings.ReplaceAll(butaneConfig, "\t", "  ")
	return []byte(butaneConfig)
}

// DoesClusterSupportNftablesTests verifies if given environment supports metalLb tests.
func DoesClusterSupportNftablesTests(requiredWorkerNodeNumber int) error {
	glog.V(90).Infof("Verifying if MetalLb operator deployed")

	if err := isMCControllerDeployed(); err != nil {
		return err
	}

	workerNodeList, err := nodes.List(
		APIClient,
		metav1.ListOptions{LabelSelector: labels.Set(NetConfig.WorkerLabelMap).String()},
	)

	if err != nil {
		return err
	}

	glog.V(90).Infof("Verifying if cluster has enough workers to run MetalLb tests")

	if len(workerNodeList) < requiredWorkerNodeNumber {
		return fmt.Errorf("cluster has less than %d worker nodes", requiredWorkerNodeNumber)
	}

	return nil
}

func isMCControllerDeployed() error {
	metalLbNS := namespace.NewBuilder(APIClient, NetConfig.MlbOperatorNamespace)
	if !metalLbNS.Exists() {
		return fmt.Errorf("error metallb namespace %s doesn't exist", metalLbNS.Definition.Name)
	}

	metalLbController, err := deployment.Pull(
		APIClient, tsparams.OperatorControllerManager, NetConfig.MlbOperatorNamespace)

	if err != nil {
		return fmt.Errorf("error to pull metallb controller deployment %s from cluster", tsparams.OperatorControllerManager)
	}

	if !metalLbController.IsReady(30 * time.Second) {
		return fmt.Errorf("error metallb controller deployment %s is not in ready/ready state",
			tsparams.OperatorControllerManager)
	}

	return nil
}
