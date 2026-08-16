package netutil

import (
	"flamerouter/internal/opensse/shared/machineid"
)

// GetConsistentMachineID delegates to opensse/shared/machineid.GetConsistentMachineID.
func GetConsistentMachineID(salt string) string {
	return machineid.GetConsistentMachineID(salt)
}
