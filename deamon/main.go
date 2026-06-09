package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
)

// ─── Estructuras ───────────────────────────────────────────────────────────────

type ProcInfo struct {
	Totalram  uint64    `json:"Totalram"`
	Freeram   uint64    `json:"Freeram"`
	Usedram   uint64    `json:"Usedram"`
	Procs     int       `json:"Procs"`
	Processes []Process `json:"Processes"`
}

type Process struct {
	PID      int    `json:"PID"`
	Name     string `json:"Name"`
	Cmdline  string `json:"Cmdline"`
	VSZ      uint64 `json:"VSZ"`
	RSS      uint64 `json:"RSS"`
	MemUsage string `json:"MemUsage"`
	CPUUsage string `json:"CPUUsage"`
}

type ContainerInfo struct {
	ID       string
	Name     string
	Image    string
	Tipo     string // "alto" o "bajo"
	RSS      uint64
	VSZ      uint64
	MemUsage float64
	CPUUsage float64
	PID      int
}

// ─── Constantes ────────────────────────────────────────────────────────────────

const (
	PROC_FILE     = "/proc/continfo_pr1_so1_202300733"
	LOOP_INTERVAL = 30 * time.Second
	MAX_ALTO      = 2
	MAX_BAJO      = 3
)

var rdb *redis.Client
var ctx = context.Background()

// ─── Main ──────────────────────────────────────────────────────────────────────

func main() {
	log.Println("=== Iniciando Daemon SO1 ===")

	levantarInfraestructura()
	conectarValkey()
	instalarCronjob()
	cargarModuloKernel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for {
			loopPrincipal()
			time.Sleep(LOOP_INTERVAL)
		}
	}()

	sig := <-sigs
	log.Printf("Señal recibida: %v — iniciando shutdown...", sig)
	shutdown()
}

// ─── Arranque ──────────────────────────────────────────────────────────────────

func levantarInfraestructura() {
	log.Println("Levantando Grafana + Valkey...")
	projectDir := "/home/rafa/Documentos/SOPES1/202300733_LAB_P1_SO1_VacJun2026"
	cmd := exec.Command("docker", "compose", "up", "-d")
	cmd.Dir = projectDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Advertencia: %v\nOutput: %s", err, output)
	} else {
		log.Println("Grafana + Valkey listos")
	}
}

func conectarValkey() {
	log.Println("Conectando con Valkey...")
	rdb = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   0,
	})
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Error conectando con Valkey: %v", err)
	}
	log.Println("Conexión con Valkey establecida")
}

func instalarCronjob() {
	log.Println("Instalando cronjob...")
	scriptPath := "/home/rafa/Documentos/SOPES1/202300733_LAB_P1_SO1_VacJun2026/cronjob/containers.sh"
	os.Chmod(scriptPath, 0755)
	logPath := scriptPath + ".log"
	comandoCron := fmt.Sprintf("*/2 * * * * %s >> %s 2>&1", scriptPath, logPath)
	cmd := exec.Command("bash", "-c",
		fmt.Sprintf("(crontab -l 2>/dev/null | grep -v containers.sh; echo \"%s\") | crontab -", comandoCron))
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Error instalando cronjob: %v\nOutput: %s", err, output)
	} else {
		log.Println("Cronjob instalado correctamente")
	}
}

func cargarModuloKernel() {
	log.Println("Cargando módulo de kernel...")
	kernelDir := "/home/rafa/Documentos/SOPES1/202300733_LAB_P1_SO1_VacJun2026/kernel"
	checkCmd := exec.Command("lsmod")
	output, _ := checkCmd.Output()
	if strings.Contains(string(output), "sysinfo") {
		log.Println("Módulo ya estaba cargado")
		return
	}
	cmd := exec.Command("sudo", "insmod", kernelDir+"/sysinfo.ko")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Error cargando módulo: %v\nOutput: %s", err, out)
	} else {
		log.Println("Módulo de kernel cargado correctamente")
	}
}

// ─── Loop Principal ────────────────────────────────────────────────────────────

func loopPrincipal() {
	log.Println("--- Iniciando iteración del loop ---")

	// 1. Leer y deserializar /proc
	data, err := os.ReadFile(PROC_FILE)
	if err != nil {
		log.Printf("Error leyendo %s: %v", PROC_FILE, err)
		return
	}

	var procInfo ProcInfo
	if err := json.Unmarshal(data, &procInfo); err != nil {
		log.Printf("Error deserializando JSON: %v", err)
		return
	}

	log.Printf("RAM Total: %d KB | Usada: %d KB | Libre: %d KB | Procesos: %d",
		procInfo.Totalram, procInfo.Usedram, procInfo.Freeram, procInfo.Procs)

	// 2. Guardar métricas de RAM
	guardarMetricasRAM(procInfo)

	// 3. Obtener contenedores activos desde Docker
	contenedores := obtenerContenedores(procInfo)
	if len(contenedores) == 0 {
		log.Println("No hay contenedores activos para analizar")
		return
	}

	// 4. Clasificar en alto y bajo consumo
	altos, bajos := clasificarContenedores(contenedores)
	log.Printf("Contenedores — Alto consumo: %d | Bajo consumo: %d", len(altos), len(bajos))

	// 5. Aplicar lógica de decisiones
	gestionarContenedores(altos, bajos)

	log.Println("--- Iteración completada ---")
}

// ─── Gestión de Contenedores ───────────────────────────────────────────────────

func obtenerContenedores(procInfo ProcInfo) []ContainerInfo {
	// Obtener lista de contenedores corriendo desde Docker
	cmd := exec.Command("docker", "ps", "--format", "{{.ID}}|{{.Names}}|{{.Image}}|{{.Command}}")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Error obteniendo contenedores: %v", err)
		return nil
	}

	var contenedores []ContainerInfo
	lineas := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, linea := range lineas {
		if linea == "" {
			continue
		}
		partes := strings.Split(linea, "|")
		if len(partes) < 4 {
			continue
		}

		id := partes[0]
		nombre := partes[1]
		imagen := partes[2]
		comando := partes[3]

		// Saltar Grafana y Valkey — nunca se eliminan
		if strings.Contains(nombre, "grafana") || strings.Contains(nombre, "valkey") {
			continue
		}

		// Clasificar tipo
		tipo := clasificarTipo(imagen, comando)

		// Buscar métricas del proceso en /proc
		rss, vsz, memUsage, cpuUsage, pid := buscarMetricasProceso(id, procInfo)

		contenedores = append(contenedores, ContainerInfo{
			ID:       id,
			Name:     nombre,
			Image:    imagen,
			Tipo:     tipo,
			RSS:      rss,
			VSZ:      vsz,
			MemUsage: memUsage,
			CPUUsage: cpuUsage,
			PID:      pid,
		})
	}

	return contenedores
}

func clasificarTipo(imagen, comando string) string {
	// go-client siempre es alto consumo
	if strings.Contains(imagen, "go-client") {
		return "alto"
	}
	// alpine con bc es alto consumo (CPU)
	if strings.Contains(comando, "bc") {
		return "alto"
	}
	// alpine con sleep es bajo consumo
	return "bajo"
}

func buscarMetricasProceso(containerID string, procInfo ProcInfo) (uint64, uint64, float64, float64, int) {
	// Obtener el PID principal del contenedor
	cmd := exec.Command("docker", "inspect", "--format", "{{.State.Pid}}", containerID)
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, 0, 0, 0
	}

	pidStr := strings.TrimSpace(string(output))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid == 0 {
		return 0, 0, 0, 0, 0
	}

	// Buscar ese PID en los procesos del módulo de kernel
	for _, proc := range procInfo.Processes {
		if proc.PID == pid {
			memUsage, _ := strconv.ParseFloat(proc.MemUsage, 64)
			cpuUsage, _ := strconv.ParseFloat(proc.CPUUsage, 64)
			return proc.RSS, proc.VSZ, memUsage, cpuUsage, pid
		}
	}

	return 0, 0, 0, 0, pid
}

func clasificarContenedores(contenedores []ContainerInfo) ([]ContainerInfo, []ContainerInfo) {
	var altos, bajos []ContainerInfo
	for _, c := range contenedores {
		if c.Tipo == "alto" {
			altos = append(altos, c)
		} else {
			bajos = append(bajos, c)
		}
	}
	return altos, bajos
}

func gestionarContenedores(altos, bajos []ContainerInfo) {
	// Ordenar por RSS descendente (más consumo primero)
	sort.Slice(altos, func(i, j int) bool {
		return altos[i].RSS > altos[j].RSS
	})
	sort.Slice(bajos, func(i, j int) bool {
		return bajos[i].RSS > bajos[j].RSS
	})

	// Eliminar excedentes de alto consumo (mantener solo MAX_ALTO=2)
	if len(altos) > MAX_ALTO {
		excedentes := altos[MAX_ALTO:] // los de mayor consumo que sobran
		for _, c := range excedentes {
			log.Printf("Eliminando contenedor ALTO consumo: %s (RSS: %d KB)", c.Name, c.RSS)
			eliminarContenedor(c)
		}
	}

	// Eliminar excedentes de bajo consumo (mantener solo MAX_BAJO=3)
	if len(bajos) > MAX_BAJO {
		excedentes := bajos[MAX_BAJO:] // los de mayor consumo que sobran
		for _, c := range excedentes {
			log.Printf("Eliminando contenedor BAJO consumo: %s (RSS: %d KB)", c.Name, c.RSS)
			eliminarContenedor(c)
		}
	}
}

func eliminarContenedor(c ContainerInfo) {
	// Detener el contenedor
	stopCmd := exec.Command("docker", "stop", c.ID)
	if err := stopCmd.Run(); err != nil {
		log.Printf("Error deteniendo %s: %v", c.Name, err)
		return
	}

	// Eliminar el contenedor
	rmCmd := exec.Command("docker", "rm", c.ID)
	if err := rmCmd.Run(); err != nil {
		log.Printf("Error eliminando %s: %v", c.Name, err)
		return
	}

	log.Printf("Contenedor eliminado: %s (%s)", c.Name, c.ID)

	// Guardar log en Valkey
	guardarLogEliminacion(c)
}

// ─── Persistencia en Valkey ────────────────────────────────────────────────────

func guardarMetricasRAM(info ProcInfo) {
	timestamp := time.Now().Unix()
	key := fmt.Sprintf("ram:%d", timestamp)

	err := rdb.HSet(ctx, key, map[string]interface{}{
		"totalram":  info.Totalram,
		"freeram":   info.Freeram,
		"usedram":   info.Usedram,
		"procs":     info.Procs,
		"timestamp": timestamp,
	}).Err()
	if err != nil {
		log.Printf("Error guardando métricas RAM: %v", err)
		return
	}

	rdb.Expire(ctx, key, 24*time.Hour)
	rdb.ZAdd(ctx, "ram:timeline", redis.Z{
		Score:  float64(timestamp),
		Member: key,
	})

	log.Printf("Métricas RAM guardadas — key: %s", key)
}

func guardarLogEliminacion(c ContainerInfo) {
	timestamp := time.Now().Unix()
	key := fmt.Sprintf("eliminado:%d:%s", timestamp, c.ID[:8])

	err := rdb.HSet(ctx, key, map[string]interface{}{
		"container_id": c.ID,
		"name":         c.Name,
		"image":        c.Image,
		"tipo":         c.Tipo,
		"rss":          c.RSS,
		"vsz":          c.VSZ,
		"mem_usage":    c.MemUsage,
		"cpu_usage":    c.CPUUsage,
		"pid":          c.PID,
		"timestamp":    timestamp,
	}).Err()
	if err != nil {
		log.Printf("Error guardando log eliminación: %v", err)
		return
	}

	rdb.Expire(ctx, key, 24*time.Hour)

	// Lista ordenada de eliminados para Grafana
	rdb.ZAdd(ctx, "eliminados:timeline", redis.Z{
		Score:  float64(timestamp),
		Member: key,
	})

	// Incrementar contador total de eliminados
	rdb.Incr(ctx, "eliminados:total")

	log.Printf("Log de eliminación guardado — key: %s", key)
}

// ─── Shutdown ──────────────────────────────────────────────────────────────────

func shutdown() {
	log.Println("Eliminando cronjob...")
	exec.Command("bash", "-c",
		"(crontab -l 2>/dev/null | grep -v containers.sh) | crontab -").Run()
	log.Println("Cronjob eliminado")

	log.Println("Descargando módulo de kernel...")
	exec.Command("sudo", "rmmod", "sysinfo").Run()
	log.Println("Módulo descargado")

	log.Println("=== Daemon detenido correctamente ===")
	os.Exit(0)
}
