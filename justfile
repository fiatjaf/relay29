dev:
    godotenv go run .

build:
    CC=musl-gcc go build -ldflags='-s -w -linkmode external -extldflags "-static"' -o ./relay29

deploy: build
    ssh root@turgot 'systemctl stop groups'
    scp relay29 turgot:groups/relay29
    ssh root@turgot 'systemctl start groups'
