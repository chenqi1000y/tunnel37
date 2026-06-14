package server

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

type CapacityPlan struct {
	CPUCores                    int    `json:"cpu_cores"`
	MemoryMB                    int    `json:"memory_mb"`
	MemoryGBLabel               string `json:"memory_gb_label"`
	RecommendedOnlineAgents     int    `json:"recommended_online_agents"`
	RecommendedActiveConcurrent int    `json:"recommended_active_concurrent"`
	RecommendedBurstConcurrent  int    `json:"recommended_burst_concurrent"`
	StabilityLevel              string `json:"stability_level"`
	UpgradeHint                 string `json:"upgrade_hint"`
}

func DetectCapacityPlan() CapacityPlan {
	cpus := runtime.NumCPU()
	if cpus <= 0 {
		cpus = 1
	}
	memoryMB := detectMemoryMB()
	if memoryMB <= 0 {
		memoryMB = 2048
	}
	memoryGB := float64(memoryMB) / 1024.0
	recommendedOnline := minInt(cpus*120, int(memoryGB*180))
	if recommendedOnline < 80 {
		recommendedOnline = 80
	}
	recommendedActive := minInt(cpus*10, int(memoryGB*15))
	if recommendedActive < 8 {
		recommendedActive = 8
	}
	recommendedBurst := minInt(cpus*15, int(memoryGB*22))
	if recommendedBurst < recommendedActive {
		recommendedBurst = recommendedActive
	}

	level := "测试级"
	hint := "当前配置更适合测试、少量接入和联调，建议将在线代理控制在 80-150、活跃并发控制在 8-15。"
	if cpus >= 4 && memoryMB >= 8192 {
		level = "商用基础级"
		hint = "当前配置可以承载小规模商用，建议将在线代理控制在 300-600、活跃并发控制在 30-60。"
	} else if cpus >= 4 && memoryMB >= 4096 {
		level = "预商用级"
		hint = "当前配置适合灰度商用，建议将在线代理控制在 180-350、活跃并发控制在 18-35。"
	}

	return CapacityPlan{
		CPUCores:                    cpus,
		MemoryMB:                    memoryMB,
		MemoryGBLabel:               formatMemoryGB(memoryMB),
		RecommendedOnlineAgents:     recommendedOnline,
		RecommendedActiveConcurrent: recommendedActive,
		RecommendedBurstConcurrent:  recommendedBurst,
		StabilityLevel:              level,
		UpgradeHint:                 hint,
	}
}

func detectMemoryMB() int {
	content, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.Atoi(fields[1])
		if err != nil || kb <= 0 {
			return 0
		}
		return kb / 1024
	}
	return 0
}

func formatMemoryGB(memoryMB int) string {
	whole := memoryMB / 1024
	decimal := (memoryMB % 1024) * 10 / 1024
	return strconv.Itoa(whole) + "." + strconv.Itoa(decimal) + " GB"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
