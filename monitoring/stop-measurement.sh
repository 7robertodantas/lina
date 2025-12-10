kill $(cat docker_stats.pid)
kill $(cat mpstat.pid)
kill $(cat pidstat.pid)
kill $(cat vmstat.pid)
kill $(cat iostat.pid)

rm docker_stats.pid mpstat.pid pidstat.pid vmstat.pid iostat.pid

echo "All monitors stopped."