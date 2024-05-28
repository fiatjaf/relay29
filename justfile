dev:
    ag -l --go | entr -r godotenv go run .

build:
    CC=musl-gcc go build -ldflags='-s -w -linkmode external -extldflags "-static"' -o ./relay29

deploy: build
    ssh root@cantillon 'systemctl stop groups'
    scp relay29 cantillon:groups/relay29
    ssh root@cantillon 'systemctl start groups'
