npm run build;


localIp=$(ifconfig en0 | grep inet | awk '$1=="inet" {print $2}');
echo "Running on local ip: $localIp";
go run main.go;