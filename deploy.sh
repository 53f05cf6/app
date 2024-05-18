git pull -r
npm install
go build
sudo kill $(sudo lsof -i :80 | awk 'NR==2 {print $2}')
sudo -b -E ./53f05cf6
