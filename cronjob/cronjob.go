package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	// Obtener ruta absoluta del script para que el cronjob funcione
	// sin importar desde dónde se ejecute
	scriptPath, err := filepath.Abs("./containers.sh")
	if err != nil {
		log.Fatal("Error obteniendo ruta absoluta:", err)
	}

	hacerEjecutable(scriptPath)
	agregarCronJob(scriptPath)
	verificarCronJob(scriptPath)
	log.Println("Cronjob configurado exitosamente")
}

// hacerEjecutable da permisos de ejecución al script bash
// 0755 = dueño puede leer/escribir/ejecutar, otros solo leer/ejecutar
func hacerEjecutable(scriptPath string) {
	err := os.Chmod(scriptPath, 0755)
	if err != nil {
		log.Fatal("Error al hacer el script ejecutable:", err)
	}
	fmt.Printf("Script %s ahora es ejecutable\n", scriptPath)
}

// agregarCronJob instala el cronjob en el sistema
// */2 * * * * significa "ejecutar cada 2 minutos"
func agregarCronJob(rutaScript string) {
	logPath := rutaScript + ".log"
	// Formato: expresion_cron comando >> archivo.log 2>&1
	expresionCron := "*/2 * * * *"
	comandoCron := fmt.Sprintf("%s %s >> %s 2>&1", expresionCron, rutaScript, logPath)

	// Encadenamos 3 operaciones:
	// 1. crontab -l → lista cronjobs existentes
	// 2. echo → agrega la nueva línea
	// 3. crontab - → instala la nueva lista completa
	cmd := exec.Command("bash", "-c",
		fmt.Sprintf("(crontab -l 2>/dev/null; echo \"%s\") | crontab -", comandoCron))

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("Error agregando cronjob: %v\nOutput: %s", err, string(output))
	}
	log.Printf("Cronjob agregado: %s", comandoCron)
}

// verificarCronJob muestra los cronjobs activos para confirmar la instalación
func verificarCronJob(rutaScript string) {
	cmd := exec.Command("crontab", "-l")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("No se pudieron listar cronjobs: %v", err)
	} else {
		log.Printf("=== Cronjobs Actuales ===\n%s=== Fin ===", string(output))
	}
}