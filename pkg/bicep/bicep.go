package bicep

import (
	"fmt"
	"os/exec"
)

func CompileBicep(bicepFile string) ([]byte, error) {
	cmd := exec.Command("bicep", "build", bicepFile, "--stdout")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("bicep build failed: %v\nOutput: %s", err, string(output))
	}
	return output, nil
}
