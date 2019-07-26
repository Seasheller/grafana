### Dependencies

- Go (Latest Stable)
  - golang (go1.12.7.windows-amd64.msi)
  - gcc  (tdm64-gcc-5.1.0-2.exe)
- Node.js LTS
  - yarn [`npm install -g yarn`]
  - yarn install 命令 需要依赖 python2.7版本 


  ### Get the project

**The project located in the go-path will be your working directory.**

```bash
go get github.com/Seasheller/grafana
cd $GOPATH/src/github.com/Seasheller/grafana
```

### Building

#### Backend

```bash
go run build.go setup
go run build.go build
```

#### Frontend

```bash
yarn install --pure-lockfile
```




