#!/bin/bash

# Imágenes disponibles
comandos=(
  "docker run -d roldyoran/go-client"
  "docker run -d alpine sh -c 'while true; do echo 2^20 | bc > /dev/null; sleep 2; done'"
  "docker run -d alpine sleep 240"
)

echo "=== Creando 5 contenedores aleatorios ==="

for i in {1..5}; do
  indice=$((RANDOM % 3))
  echo "Contenedor $i: ejecutando opcion $((indice + 1)) de 3..."
  eval "${comandos[$indice]}"
done

echo "=== 5 contenedores creados ==="