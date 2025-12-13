all:
	go run *.go;

build:
	./node_modules/.bin/rolldown src/index.ts --file ./public/assets/notflix.js;
	go build -o notflix *.go;

dev:
	./node_modules/.bin/rolldown src/index.ts --file ./public/assets/notflix.js --watch
