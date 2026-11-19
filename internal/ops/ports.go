package ops

import (
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ProcessInfo struct {
	PID         int    `json:"pid"`
	ProcessName string `json:"process_name"`
}

type DockerPortBinding struct {
	HostPort      int    `json:"host_port"`
	ContainerName string `json:"container_name"`
	Image         string `json:"image"`
	Status        string `json:"status"`
}

type PortReport struct {
	Port          int    `json:"port"`
	Open          bool   `json:"open"`
	LatencyMs     int64  `json:"latency_ms"`
	PID           int    `json:"pid"`
	ProcessName   string `json:"process_name"`
	ContainerName string `json:"container_name"`
	Image         string `json:"image"`
	Status        string `json:"status"`
}

func getWindowsListeners() map[int]ProcessInfo {
	result := make(map[int]ProcessInfo)

	psCmd := `Get-NetTCPConnection -State Listen | Select-Object LocalPort,OwningProcess | ForEach-Object { $p = Get-Process -Id $_.OwningProcess -ErrorAction SilentlyContinue; [PSCustomObject]@{ port = $_.LocalPort; pid = $_.OwningProcess; name = $p.ProcessName } } | ConvertTo-Json -Compress`

	out, err := exec.Command("powershell", "-NoProfile", "-Command", psCmd).Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return result
	}

	raw := strings.TrimSpace(string(out))
	var items []struct {
		Port int     `json:"port"`
		PID  int     `json:"pid"`
		Name *string `json:"name"`
	}

	if strings.HasPrefix(raw, "[") {
		_ = json.Unmarshal([]byte(raw), &items)
	} else {
		var single struct {
			Port int     `json:"port"`
			PID  int     `json:"pid"`
			Name *string `json:"name"`
		}
		if err := json.Unmarshal([]byte(raw), &single); err == nil && single.Port > 0 {
			items = append(items, single)
		}
	}

	for _, it := range items {
		name := ""
		if it.Name != nil {
			name = *it.Name
		}
		result[it.Port] = ProcessInfo{
			PID:         it.PID,
			ProcessName: name,
		}
	}
	return result
}

func getDockerPortBindings() map[int]DockerPortBinding {
	bindings := make(map[int]DockerPortBinding)

	out, err := exec.Command("docker", "ps", "-a", "--format", "{{json .}}").Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return bindings
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var c struct {
			Names  string `json:"Names"`
			Image  string `json:"Image"`
			Status string `json:"Status"`
			Ports  string `json:"Ports"`
		}
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue
		}

		for _, part := range strings.Split(c.Ports, ",") {
			part = strings.TrimSpace(part)
			if !strings.Contains(part, "->") {
				continue
			}
			hostSide := strings.Split(part, "->")[0]
			idx := strings.LastIndex(hostSide, ":")
			if idx == -1 {
				continue
			}
			portStr := strings.TrimSpace(hostSide[idx+1:])
			hp, err := strconv.Atoi(portStr)
			if err == nil && hp > 0 {
				bindings[hp] = DockerPortBinding{
					HostPort:      hp,
					ContainerName: c.Names,
					Image:         c.Image,
					Status:        c.Status,
				}
			}
		}
	}
	return bindings
}

func ScanActivePorts(host string, timeout time.Duration) []PortReport {
	listeners := getWindowsListeners()
	dockerMap := getDockerPortBindings()

	portSet := make(map[int]struct{})
	for p := range listeners {
		portSet[p] = struct{}{}
	}
	for p := range dockerMap {
		portSet[p] = struct{}{}
	}

	ports := make([]int, 0, len(portSet))
	for p := range portSet {
		ports = append(ports, p)
	}
	sort.Ints(ports)

	reports := make([]PortReport, 0, len(ports))
	var mu sync.Mutex
	var wg sync.WaitGroup

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, p := range ports {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()

			start := time.Now()
			dialCtx, dCancel := context.WithTimeout(ctx, timeout)
			defer dCancel()

			var d net.Dialer
			conn, err := d.DialContext(dialCtx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
			latency := time.Since(start).Milliseconds()

			proc, hasProc := listeners[port]
			dock, hasDock := dockerMap[port]

			rep := PortReport{
				Port:      port,
				Open:      err == nil,
				LatencyMs: latency,
			}
			if err == nil {
				_ = conn.Close()
			}
			if hasProc {
				rep.PID = proc.PID
				rep.ProcessName = proc.ProcessName
			}
			if hasDock {
				rep.ContainerName = dock.ContainerName
				rep.Image = dock.Image
				rep.Status = dock.Status
			}

			if !rep.Open && rep.ContainerName == "" {
				return
			}

			mu.Lock()
			reports = append(reports, rep)
			mu.Unlock()
		}(p)
	}
	wg.Wait()

	sort.Slice(reports, func(i, j int) bool {
		return reports[i].Port < reports[j].Port
	})
	return reports
}