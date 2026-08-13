package collector

import (
	"testing"

	gpsdisk "github.com/shirou/gopsutil/v4/disk"
)

func TestIsPseudoMount(t *testing.T) {
	cases := []struct {
		name   string
		part   gpsdisk.PartitionStat
		pseudo bool
	}{
		{"raíz real ext4", gpsdisk.PartitionStat{Mountpoint: "/", Fstype: "ext4"}, false},
		{"disco real de Windows", gpsdisk.PartitionStat{Mountpoint: `C:\`, Fstype: "NTFS"}, false},
		{"tmpfs por fstype", gpsdisk.PartitionStat{Mountpoint: "/dev/shm", Fstype: "tmpfs"}, true},
		{"overlay de Docker", gpsdisk.PartitionStat{Mountpoint: "/var/lib/docker/overlay2/xxx/merged", Fstype: "overlay"}, true},
		{"/proc exacto", gpsdisk.PartitionStat{Mountpoint: "/proc", Fstype: "proc"}, true},
		{"subruta de /proc", gpsdisk.PartitionStat{Mountpoint: "/proc/sys/fs/binfmt_misc", Fstype: "binfmt_misc"}, true},
		{"prefijo parecido pero NO es subruta (regresión del fix strings.HasPrefix)", gpsdisk.PartitionStat{Mountpoint: "/proceso-legitimo", Fstype: "ext4"}, false},
		{"snap", gpsdisk.PartitionStat{Mountpoint: "/snap/core20/1234", Fstype: "squashfs"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isPseudoMount(c.part); got != c.pseudo {
				t.Errorf("isPseudoMount(%+v) = %v, se esperaba %v", c.part, got, c.pseudo)
			}
		})
	}
}
