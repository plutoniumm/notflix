localIp=$(ifconfig en0 | grep inet | awk '$1=="inet" {print $2}');
echo "Running on local ip: $localIp:5173";
go run main.go;