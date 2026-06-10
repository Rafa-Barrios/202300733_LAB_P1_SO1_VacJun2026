package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
	iniciarHTTPServer()

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

	// Guardar Top 5 para Grafana
	guardarTop5(contenedores)

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
	// Guardar valores numéricos directos para gráficas
	rdb.ZAdd(ctx, "ts:usedram", redis.Z{
		Score:  float64(timestamp),
		Member: fmt.Sprintf("%d:%d", timestamp*1000, info.Usedram),
	})
	rdb.ZAdd(ctx, "ts:freeram", redis.Z{
		Score:  float64(timestamp),
		Member: fmt.Sprintf("%d:%d", timestamp*1000, info.Freeram),
	})
	rdb.Expire(ctx, "ts:usedram", 24*time.Hour)
	rdb.Expire(ctx, "ts:freeram", 24*time.Hour)

	// Guardar referencia al último snapshot
	rdb.Set(ctx, "ram:latest", key, 24*time.Hour)

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

	rdb.ZAdd(ctx, "ts:eliminados", redis.Z{
		Score:  float64(timestamp),
		Member: fmt.Sprintf("%d:1", timestamp),
	})
	rdb.Expire(ctx, "ts:eliminados", 24*time.Hour)

	log.Printf("Log de eliminación guardado — key: %s", key)
}

func guardarTop5(contenedores []ContainerInfo) {
	if len(contenedores) == 0 {
		return
	}

	timestamp := time.Now().Unix()

	// Ordenar por RSS para Top 5 RAM
	porRAM := make([]ContainerInfo, len(contenedores))
	copy(porRAM, contenedores)
	sort.Slice(porRAM, func(i, j int) bool {
		return porRAM[i].RSS > porRAM[j].RSS
	})

	// Ordenar por CPU para Top 5 CPU
	porCPU := make([]ContainerInfo, len(contenedores))
	copy(porCPU, contenedores)
	sort.Slice(porCPU, func(i, j int) bool {
		return porCPU[i].CPUUsage > porCPU[j].CPUUsage
	})

	// Limitar a 5
	limiteRAM := 5
	if len(porRAM) < 5 {
		limiteRAM = len(porRAM)
	}
	limiteCPU := 5
	if len(porCPU) < 5 {
		limiteCPU = len(porCPU)
	}

	// Guardar Top 5 RAM
	keyRAM := fmt.Sprintf("top5ram:%d", timestamp)
	for i, c := range porRAM[:limiteRAM] {
		rdb.HSet(ctx, keyRAM, fmt.Sprintf("rank%d", i+1),
			fmt.Sprintf("%s|%s|%d", c.ID[:8], c.Name, c.RSS))
	}
	rdb.Expire(ctx, keyRAM, 24*time.Hour)
	rdb.Set(ctx, "top5ram:latest", keyRAM, 24*time.Hour)

	// Guardar Top 5 CPU
	keyCPU := fmt.Sprintf("top5cpu:%d", timestamp)
	for i, c := range porCPU[:limiteCPU] {
		rdb.HSet(ctx, keyCPU, fmt.Sprintf("rank%d", i+1),
			fmt.Sprintf("%s|%s|%.2f", c.ID[:8], c.Name, c.CPUUsage))
	}
	rdb.Expire(ctx, keyCPU, 24*time.Hour)
	rdb.Set(ctx, "top5cpu:latest", keyCPU, 24*time.Hour)

	log.Printf("Top 5 guardado — RAM key: %s | CPU key: %s", keyRAM, keyCPU)
}

func iniciarHTTPServer() {
	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Obtener últimos 20 valores de RAM
		vals, err := rdb.ZRevRangeWithScores(ctx, "ts:usedram", 0, 19).Result()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		type DataPoint struct {
			Time  float64 `json:"time"`
			Value float64 `json:"value"`
		}

		var points []DataPoint
		for _, v := range vals {
			parts := strings.Split(v.Member.(string), ":")
			if len(parts) == 2 {
				val, _ := strconv.ParseFloat(parts[1], 64)
				points = append(points, DataPoint{
					Time:  v.Score,
					Value: val,
				})
			}
		}

		json.NewEncoder(w).Encode(points)
	})

	http.HandleFunc("/ram", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		vals, err := rdb.ZRangeWithScores(ctx, "ts:usedram", 0, -1).Result()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		type DataPoint struct {
			Time  string  `json:"time"`
			Value float64 `json:"value"`
		}

		var points []DataPoint
		for _, v := range vals {
			parts := strings.Split(v.Member.(string), ":")
			if len(parts) == 2 {
				val, _ := strconv.ParseFloat(parts[1], 64)
				ts := time.Unix(int64(v.Score), 0)
				points = append(points, DataPoint{
					Time:  ts.UTC().Format(time.RFC3339),
					Value: val,
				})
			}
		}

		json.NewEncoder(w).Encode(points)
	})

	http.HandleFunc("/eliminados", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		vals, err := rdb.ZRangeWithScores(ctx, "ts:eliminados", 0, -1).Result()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		type DataPoint struct {
			Time  string `json:"time"`
			Count int    `json:"count"`
		}

		var points []DataPoint
		for _, v := range vals {
			ts := time.Unix(int64(v.Score), 0)
			points = append(points, DataPoint{
				Time:  ts.UTC().Format(time.RFC3339),
				Count: 1,
			})
		}

		json.NewEncoder(w).Encode(points)
	})

	http.HandleFunc("/top5ram", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		key, err := rdb.Get(ctx, "top5ram:latest").Result()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		vals, err := rdb.HGetAll(ctx, key).Result()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		type Entry struct {
			Name  string  `json:"name"`
			Value float64 `json:"value"`
		}

		var entries []Entry
		for _, v := range vals {
			parts := strings.Split(v, "|")
			if len(parts) == 3 {
				val, _ := strconv.ParseFloat(parts[2], 64)
				entries = append(entries, Entry{
					Name:  parts[1],
					Value: val,
				})
			}
		}

		json.NewEncoder(w).Encode(entries)
	})

	http.HandleFunc("/top5cpu", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		key, err := rdb.Get(ctx, "top5cpu:latest").Result()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		vals, err := rdb.HGetAll(ctx, key).Result()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		type Entry struct {
			Name  string  `json:"name"`
			Value float64 `json:"value"`
		}

		var entries []Entry
		for _, v := range vals {
			parts := strings.Split(v, "|")
			if len(parts) == 3 {
				val, _ := strconv.ParseFloat(parts[2], 64)
				entries = append(entries, Entry{
					Name:  parts[1],
					Value: val,
				})
			}
		}

		json.NewEncoder(w).Encode(entries)
	})

	log.Println("HTTP server iniciado en puerto 8080")
	go http.ListenAndServe(":8080", nil)
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
