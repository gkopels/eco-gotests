package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	butaneConfig "github.com/coreos/butane/config"
	"github.com/coreos/butane/config/common"
	. "github.com/onsi/gomega"
	mc "github.com/openshift-kni/eco-goinfra/pkg/mco"
	. "github.com/openshift-kni/eco-gotests/tests/cnf/core/network/internal/netinittools"
	machineconfigurationv1 "github.com/openshift/api/machineconfiguration/v1"
	"sigs.k8s.io/yaml"
)

const butaneVersion = "4.17.0"

func CreateMC(nodeRole, artifactsDir string) (machineConfig []byte, err error) {
	butaneConfigVar := createButaneConfig(nodeRole)
	options := common.TranslateBytesOptions{}
	machineConfig, _, err = butaneConfig.TranslateBytes(butaneConfigVar, options)
	if err != nil {
		return nil, fmt.Errorf("failed to covert the ButaneConfig to yaml %v: ", err)
	}
	nodeName := "worker-0"

	// Ensure the artifacts directory exists
	if _, err := os.Stat(artifactsDir); os.IsNotExist(err) {
		if err := os.MkdirAll(artifactsDir, 0755); err != nil {
			Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("failed to create artifacts directory: %s", artifactsDir))
		}
	}
	// Write the machine config to a file
	filePath := filepath.Join(artifactsDir, "nftables-after-reboot-"+nodeName)
	if err := os.WriteFile(filePath, machineConfig, 0644); err != nil {
		return nil, fmt.Errorf("failed to write machine config to file: %w", err)
	}

	fmt.Println("machineConfig", string(machineConfig))

	return machineConfig, nil
}

func createButaneConfig(nodeRole string) []byte {
	butaneConfig := fmt.Sprintf(`variant: openshift
version: %s
metadata:
  name: 98-nftables-custom-filter-%s
  labels:
    machineconfiguration.openshift.io/role: %s
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
          }`, butaneVersion, nodeRole, nodeRole)
	butaneConfig = strings.ReplaceAll(butaneConfig, "\t", "  ")
	return []byte(butaneConfig)
}

func ApplyMachineConfig(yamlInput []byte) error {
	obj := &machineconfigurationv1.MachineConfig{}
	if err := yaml.Unmarshal(yamlInput, obj); err != nil {
		return fmt.Errorf("failed to unmarshal YAML: %w", err)
	}

	//err := c.(context.TODO(), obj)
	//if err != nil {
	//	if !errors.IsAlreadyExists(err) {
	//		return fmt.Errorf("failed to create MachineConfig: %w", err)
	//	}

	// If it already exists, retrieve the current version to update
	//existingMC := &machineconfigurationv1.MachineConfig{}
	//if err = c.Get(context.TODO(), k8sTypes.NamespacedName{Name: obj.Name}, existingMC); err != nil {
	//	return fmt.Errorf("failed to get existing MachineConfig: %w", err)
	//}

	//	obj.ResourceVersion = existingMC.ResourceVersion
	_, err := mc.NewMCBuilder(APIClient, "98-nftables-custom-filter").WithRawConfig(yamlInput).Create()
	if err != nil {
		return fmt.Errorf("failed to create MachineConfig")
	}
	//	log.Println("MachineConfig updated successfully.")
	//} else {
	//	log.Println("MachineConfig created successfully.")
	//}

	return nil
}
