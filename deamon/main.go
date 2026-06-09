package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
)

// ─── Estructuras para deserializar el JSON de /proc ───────────────────────────

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

// ─── Variables globales ────────────────────────────────────────────────────────

const (
	PROC_FILE     = "/proc/continfo_pr1_so1_202300733"
	LOOP_INTERVAL = 30 * time.Second
)

var rdb *redis.Client
var ctx = context.Background()

// ─── Main ──────────────────────────────────────────────────────────────────────

func main() {
	log.Println("=== Iniciando Daemon SO1 ===")

	// 1. Levantar Grafana + Valkey
	levantarInfraestructura()

	// 2. Conectar con Valkey
	conectarValkey()

	// 3. Instalar cronjob
	instalarCronjob()

	// 4. Cargar módulo de kernel
	cargarModuloKernel()

	// 5. Manejar señales para shutdown limpio
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// 6. Loop principal en goroutine separada
	go func() {
		for {
			loopPrincipal()
			time.Sleep(LOOP_INTERVAL)
		}
	}()

	// Esperar señal de cierre
	sig := <-sigs
	log.Printf("Señal recibida: %v — iniciando shutdown...", sig)
	shutdown()
}

// ─── Funciones de arranque ─────────────────────────────────────────────────────

func levantarInfraestructura() {
	log.Println("Levantando Grafana + Valkey...")
	projectDir := "/home/rafa/Documentos/SOPES1/202300733_LAB_P1_SO1_VacJun2026"
	cmd := exec.Command("docker", "compose", "up", "-d")
	cmd.Dir = projectDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Advertencia al levantar infraestructura: %v\nOutput: %s", err, output)
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

	// Dar permisos de ejecución
	os.Chmod(scriptPath, 0755)

	// Instalar cronjob cada 2 minutos
	logPath := scriptPath + ".log"
	expresionCron := "*/2 * * * *"
	comandoCron := fmt.Sprintf("%s %s >> %s 2>&1", expresionCron, scriptPath, logPath)

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

	// Verificar si ya está cargado
	checkCmd := exec.Command("lsmod")
	output, _ := checkCmd.Output()
	if contains(string(output), "sysinfo") {
		log.Println("Módulo ya estaba cargado")
		return
	}

	// Cargar el módulo
	cmd := exec.Command("sudo", "insmod", kernelDir+"/sysinfo.ko")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Error cargando módulo: %v\nOutput: %s", err, out)
	} else {
		log.Println("Módulo de kernel cargado correctamente")
	}
}

// ─── Loop principal ────────────────────────────────────────────────────────────

func loopPrincipal() {
	log.Println("--- Iniciando iteración del loop ---")

	// Leer /proc
	data, err := os.ReadFile(PROC_FILE)
	if err != nil {
		log.Printf("Error leyendo %s: %v", PROC_FILE, err)
		return
	}

	// Deserializar JSON
	var procInfo ProcInfo
	if err := json.Unmarshal(data, &procInfo); err != nil {
		log.Printf("Error deserializando JSON: %v", err)
		return
	}

	log.Printf("RAM Total: %d KB | Usada: %d KB | Libre: %d KB | Procesos: %d",
		procInfo.Totalram, procInfo.Usedram, procInfo.Freeram, procInfo.Procs)

	// Guardar métricas de RAM en Valkey
	guardarMetricasRAM(procInfo)

	log.Println("--- Iteración completada ---")
}

// ─── Funciones de Valkey ───────────────────────────────────────────────────────

func guardarMetricasRAM(info ProcInfo) {
	timestamp := time.Now().Unix()

	// Guardar snapshot de RAM como hash
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

	// Expiración de 24 horas para no llenar la memoria
	rdb.Expire(ctx, key, 24*time.Hour)

	// Agregar timestamp a lista ordenada para consultas temporales
	rdb.ZAdd(ctx, "ram:timeline", redis.Z{
		Score:  float64(timestamp),
		Member: key,
	})

	log.Printf("Métricas RAM guardadas en Valkey con key: %s", key)
}

// ─── Shutdown ──────────────────────────────────────────────────────────────────

func shutdown() {
	log.Println("Eliminando cronjob...")
	cmd := exec.Command("bash", "-c",
		"(crontab -l 2>/dev/null | grep -v containers.sh) | crontab -")
	cmd.CombinedOutput()
	log.Println("Cronjob eliminado")

	log.Println("Descargando módulo de kernel...")
	exec.Command("sudo", "rmmod", "sysinfo").Run()
	log.Println("Módulo descargado")

	log.Println("=== Daemon detenido correctamente ===")
	os.Exit(0)
}

// ─── Utilidades ────────────────────────────────────────────────────────────────

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
