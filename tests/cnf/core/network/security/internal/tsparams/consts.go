package tsparams

const (
	// LabelSuite represents nftables label that can be used for test cases selection.
	LabelSuite = "nftables"
	// LabelNftablesTestCases represents nftables custom firewall label that can be used for test cases selection.
	LabelNftablesTestCases = "nftables-custom-rules"
	// CustomFireWallDelete removes all the rules from the custom table.
	// CustomFireWallDelete = `
	//      table inet custom_table
	//      delete table inet custom_table
	//      table inet custom_table {
	//      }`
	CustomFireWallDelete = `data:;base64, ` +
		`H4sIAAAAAAAC/ypJTMpJVcjMSy1RSC4tLsnPjQeLcKWk5qSWpCrgksYhrlDNVcsFCAAA//9SII3uUwAAAA==`
	// CustomFirewallInputPort8888 adds an input rule blocking TCP port 8888.
	// chain custom_chain_INPUT {
	//    type filter hook input priority 1; policy accept;
	//     # Drop TCP port 8888 and log
	//      tcp dport 8888 log prefix "[USERFIREWALL] PACKET DROP: " drop
	// This yaml is created using butane file.
	// butane mc-update-input-rule8888.bu -o mc-custom-firewall-input-port8888.yaml.
	CustomFirewallIngressPort8888 = `data:;base64,` +
		`H4sIAAAAAAAC/3TMwUoDMRDG8XPyFB/1Cbwt9lTaFYpFl3WLB5ESk2k7GDNDnIKL` +
		`9N2lBfG0x+//g8/CeyZwIUM8fZl87q7FJ8pkhCme6PjxLh4Dl796Hbv1Y7cdLuZs` +
		`VMKes1HFUeQDXPRk0MpS2UbczqGSOY4IMZLa3Dt3g1UVxbDsoFINTdM0CCUhy+Fy` +
		`GRXpH7IcoJX2/I3Z6/a57e/Xffuy2Gze0C2WD+2AVf/U3WGGVEW9O/uz/w0AAP//` +
		`kU0CJQUBAAA=`
	// CustomFirewallIngressEgressPort8888 adds an output rule blocking TCP port 8088.
	// chain custom_chain_INPUT {
	//      type filter hook input priority 1; policy accept;
	//	  # Drop TCP port 8888 and log
	//	  tcp dport 8888 log prefix "[USERFIREWALL] PACKET DROP: " drop
	//	}
	//	chain custom_chain_OUTPUT {
	//	  type filter hook output priority 1; policy accept;
	//	  # Drop TCP port 8888 and log
	//	  tcp dport 8888 log prefix "[USERFIREWALL] PACKET DROP: " drop
	//	}
	CustomFirewallIngressEgressPort8888 = `data:;base64,` +
		`H4sIAAAAAAAC/6TOQUvDQBAF4PPur3jUX+At2FNpIxSLDTHBg0iJm2m7uO4M6wQM` +
		`kv8uTRUPWj10b/se8/i0eQoEH0nhulfll82Y2JYCKeFUfSLHuzVu3/j4lY6fzfK2` +
		`qKtDZ7QXwtYHpYQ98zN8lE4hyXPy2uNyCuHgXY/GORKdWmMusEgsqOYFhJMiy7IM` +
		`TWwReHeYdIL2uwi8gyTa+jdMHuq7vLxelvn9bLV6RDGb3+QVFuW6uMIEbWKxZvhV` +
		`vK6rIxnj+8HmTv9xHw//tH9un+UHBjt8BAAA//9jfGxtxQEAAA==`
	// FRRBaseConfig represents FRR daemon minimal configuration.
	FRRBaseConfig = `!
frr defaults traditional
hostname frr-pod
log file /tmp/frr.log
log timestamp precision 3
!
debug zebra nht
debug bgp neighbor-events
!
bfd
!
`

	// FRRDefaultBGPPreConfig represents FRR daemon BGP minimal config.
	FRRDefaultBGPPreConfig = ` bgp router-id 10.10.10.11
  no bgp ebgp-requires-policy
  no bgp default ipv4-unicast
  no bgp network import-check
`
	// DaemonsFile represents FRR default daemon configuration template.
	DaemonsFile = `
	# This file tells the frr package which daemons to start.
    #
    # Sample configurations for these daemons can be found in
    # /usr/share/doc/frr/examples/.
    #
    # ATTENTION:
    #
    # When activating a daemon for the first time, a config file, even if it is
    # empty, has to be present *and* be owned by the user and group "frr", else
    # the daemon will not be started by /etc/init.d/frr. The permissions should
    # be u=rw,g=r,o=.
    # When using "vtysh" such a config file is also needed. It should be owned by
    # group "frrvty" and set to ug=rw,o= though. Check /etc/pam.d/frr, too.
    #
    # The watchfrr, zebra and staticd daemons are always started.
    #
    bgpd=yes
    ospfd=no
    ospf6d=no
    ripd=no
    ripngd=no
    isisd=no
    pimd=no
    ldpd=no
    nhrpd=no
    eigrpd=no
    babeld=no
    sharpd=no
    pbrd=no
    bfdd=yes
    fabricd=no
    vrrpd=no
    pathd=no
    #
    # If this option is set the /etc/init.d/frr script automatically loads
    # the config via "vtysh -b" when the servers are started.
    # Check /etc/pam.d/frr if you intend to use "vtysh"!
    #
    vtysh_enable=yes
    zebra_options="  -A 127.0.0.1 -s 90000000"
    bgpd_options="   -A 127.0.0.1"
    ospfd_options="  -A 127.0.0.1"
    ospf6d_options=" -A ::1"
    ripd_options="   -A 127.0.0.1"
    ripngd_options=" -A ::1"
    isisd_options="  -A 127.0.0.1"
    pimd_options="   -A 127.0.0.1"
    ldpd_options="   -A 127.0.0.1"
    nhrpd_options="  -A 127.0.0.1"
    eigrpd_options=" -A 127.0.0.1"
    babeld_options=" -A 127.0.0.1"
    sharpd_options=" -A 127.0.0.1"
    pbrd_options="   -A 127.0.0.1"
    staticd_options="-A 127.0.0.1"
    bfdd_options="   -A 127.0.0.1"
    fabricd_options="-A 127.0.0.1"
    vrrpd_options="  -A 127.0.0.1"
    pathd_options="  -A 127.0.0.1"
    # configuration profile
    #
    #frr_profile="traditional"
    #frr_profile="datacenter"
    #
    # This is the maximum number of FD's that will be available.
    # Upon startup this is read by the control files and ulimit
    # is called.  Uncomment and use a reasonable value for your
    # setup if you are expecting a large number of peers in
    # say BGP.
    #MAX_FDS=1024
    # The list of daemons to watch is automatically generated by the init script.
    #watchfrr_options=""
    # To make watchfrr create/join the specified netns, use the following option:
    #watchfrr_options="--netns"
    # This only has an effect in /etc/frr/<somename>/daemons, and you need to
    # start FRR with "/usr/lib/frr/frrinit.sh start <somename>".
    # for debugging purposes, you can specify a "wrap" command to start instead
    # of starting the daemon directly, e.g. to use valgrind on ospfd:
    #   ospfd_wrap="/usr/bin/valgrind"
    # or you can use "all_wrap" for all daemons, e.g. to use perf record:
    #   all_wrap="/usr/bin/perf record --call-graph -"
    # the normal daemon command is added to this at the end.
	`
)
