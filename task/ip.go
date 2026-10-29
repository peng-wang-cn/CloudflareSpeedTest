package task

import (
	"bufio"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

const defaultInputFile = "ip.txt"

var (
	// TestAll test all ip
	TestAll = false
	// IPFile is the filename of IP Rangs
	IPFile = defaultInputFile
	IPText string
)

func InitRandSeed() {
	rand.Seed(time.Now().UnixNano())
}

func isIPv4(ip string) bool {
	return strings.Contains(ip, ".")
}

func randIPEndWith(num byte) byte {
	if num == 0 { // 对于 /32 这种单独的 IP
		return byte(0)
	}
	return byte(rand.Intn(int(num)))
}

type IPRanges struct {
	ips     []*net.IPAddr
	mask    string
	firstIP net.IP
	ipNet   *net.IPNet
}

func newIPRanges() *IPRanges {
	return &IPRanges{
		ips: make([]*net.IPAddr, 0),
	}
}

// 如果是单独 IP 则加上子网掩码，反之则获取子网掩码(r.mask)
func (r *IPRanges) fixIP(ip string) string {
	// 如果不含有 '/' 则代表不是 IP 段，而是一个单独的 IP，因此需要加上 /32 /128 子网掩码
	if i := strings.IndexByte(ip, '/'); i < 0 {
		if isIPv4(ip) {
			r.mask = "/32"
		} else {
			r.mask = "/128"
		}
		ip += r.mask
	} else {
		r.mask = ip[i:]
	}
	return ip
}

// 解析 IP 段，获得 IP、IP 范围、子网掩码
func (r *IPRanges) parseCIDR(ip string) {
	var err error
	if r.firstIP, r.ipNet, err = net.ParseCIDR(r.fixIP(ip)); err != nil {
		log.Fatalln("ParseCIDR err", err)
	}
}

func (r *IPRanges) appendIPv4(d byte) {
	r.appendIP(net.IPv4(r.firstIP[12], r.firstIP[13], r.firstIP[14], d))
}

func (r *IPRanges) appendIP(ip net.IP) {
	r.ips = append(r.ips, &net.IPAddr{IP: ip})
}

// 返回第四段 ip 的最小值及可用数目
func (r *IPRanges) getIPRange() (minIP, hosts byte) {
	minIP = r.firstIP[15] & r.ipNet.Mask[3] // IP 第四段最小值

	// 根据子网掩码获取主机数量
	m := net.IPv4Mask(255, 255, 255, 255)
	for i, v := range r.ipNet.Mask {
		m[i] ^= v
	}
	total, _ := strconv.ParseInt(m.String(), 16, 32) // 总可用 IP 数
	if total > 255 {                                 // 矫正 第四段 可用 IP 数
		hosts = 255
		return
	}
	hosts = byte(total)
	return
}

func (r *IPRanges) chooseIPv4() {
	if r.mask == "/32" { // 单个 IP 则无需随机，直接加入自身即可
		r.appendIP(r.firstIP)
	} else {
		minIP, hosts := r.getIPRange()    // 返回第四段 IP 的最小值及可用数目
		for r.ipNet.Contains(r.firstIP) { // 只要该 IP 没有超出 IP 网段范围，就继续循环随机
			if TestAll { // 如果是测速全部 IP
				for i := 0; i <= int(hosts); i++ { // 遍历 IP 最后一段最小值到最大值
					r.appendIPv4(byte(i) + minIP)
				}
			} else { // 随机 IP 的最后一段 0.0.0.X
				r.appendIPv4(minIP + randIPEndWith(hosts))
			}
			r.firstIP[14]++ // 0.0.(X+1).X
			if r.firstIP[14] == 0 {
				r.firstIP[13]++ // 0.(X+1).X.X
				if r.firstIP[13] == 0 {
					r.firstIP[12]++ // (X+1).X.X.X
				}
			}
		}
	}
}

func (r *IPRanges) chooseIPv6() {
	if r.mask == "/128" { // 单个 IP 则无需随机，直接加入自身即可
		r.appendIP(r.firstIP)
	} else {
		var tempIP uint8                  // 临时变量，用于记录前一位的值
		for r.ipNet.Contains(r.firstIP) { // 只要该 IP 没有超出 IP 网段范围，就继续循环随机
			r.firstIP[15] = randIPEndWith(255) // 随机 IP 的最后一段
			r.firstIP[14] = randIPEndWith(255) // 随机 IP 的最后一段

			targetIP := make([]byte, len(r.firstIP))
			copy(targetIP, r.firstIP)
			r.appendIP(targetIP) // 加入 IP 地址池

			for i := 13; i >= 0; i-- { // 从倒数第三位开始往前随机
				tempIP = r.firstIP[i]              // 保存前一位的值
				r.firstIP[i] += randIPEndWith(255) // 随机 0~255，加到当前位上
				if r.firstIP[i] >= tempIP {        // 如果当前位的值大于等于前一位的值，说明随机成功了，可以退出该循环
					break
				}
			}
		}
	}
}

func loadIPRanges() []*net.IPAddr {
	ranges := newIPRanges()
	if IPText != "" { // 从参数中获取 IP 段数据
		IPs := strings.Split(IPText, ",") // 以逗号分隔为数组并循环遍历
		for _, IP := range IPs {
			IP = strings.TrimSpace(IP) // 去除首尾的空白字符（空格、制表符、换行符等）
			if IP == "" {              // 跳过空的（即开头、结尾或连续多个 ,, 的情况）
				continue
			}
			ranges.parseCIDR(IP) // 解析 IP 段，获得 IP、IP 范围、子网掩码
			if isIPv4(IP) {      // 生成要测速的所有 IPv4 / IPv6 地址（单个/随机/全部）
				ranges.chooseIPv4()
			} else {
				ranges.chooseIPv6()
			}
		}
	} else { // 从文件中获取 IP 段数据
		if IPFile == "" {
			IPFile = defaultInputFile
		}
		file, err := os.Open(IPFile)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() { // 循环遍历文件每一行
			line := strings.TrimSpace(scanner.Text()) // 去除首尾的空白字符（空格、制表符、换行符等）
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") { // 跳过空行及注释
				continue
			}
			// 如果是 CSV 格式（含有逗号），提取第一列 IP
			if strings.Contains(line, ",") {
				parts := strings.Split(line, ",")
				col0 := strings.TrimSpace(parts[0])
				// 跳过 CSV 表头
				if col0 == "IP 地址" || strings.EqualFold(col0, "IP") || strings.EqualFold(col0, "IP Address") {
					continue
				}
				line = col0
			}
			ranges.parseCIDR(line) // 解析 IP 段，获得 IP、IP 范围、子网掩码
			if isIPv4(line) {      // 生成要测速的所有 IPv4 / IPv6 地址（单个/随机/全部）
				ranges.chooseIPv4()
			} else {
				ranges.chooseIPv6()
			}
		}
	}
	return ranges.ips
}

// LoadPingData 从文件或参数中加载 Ping 结构数据，用于禁用延迟测速(-dp)直接进行下载测速
func LoadPingData() utils.PingDelaySet {
	var pingData utils.PingDelaySet
	// 如果是指定的 IP 文件，优先检查是否是包含完整测速信息的 CSV 文件
	if IPText == "" && IPFile != "" {
		if file, err := os.Open(IPFile); err == nil {
			defer file.Close()
			scanner := bufio.NewScanner(file)
			isCSV := false
			firstLine := true
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
					continue
				}
				parts := strings.Split(line, ",")
				if firstLine {
					firstLine = false
					// 检测是否是 CloudflareSpeedTest 生成的 CSV 表头
					if len(parts) >= 5 && (parts[0] == "IP 地址" || strings.EqualFold(parts[0], "IP") || strings.EqualFold(parts[0], "IP Address")) {
						isCSV = true
						continue
					}
					// 或者是无表头但有多列的 CSV 行
					if len(parts) >= 5 && net.ParseIP(strings.TrimSpace(parts[0])) != nil {
						isCSV = true
					}
				}
				if isCSV {
					ipStr := strings.TrimSpace(parts[0])
					if ipStr == "IP 地址" || strings.EqualFold(ipStr, "IP") || strings.EqualFold(ipStr, "IP Address") {
						continue
					}
					ip := net.ParseIP(ipStr)
					if ip == nil {
						continue
					}
					sended := 0
					received := 0
					var delay time.Duration
					colo := ""
					if len(parts) > 1 {
						sended, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
					}
					if len(parts) > 2 {
						received, _ = strconv.Atoi(strings.TrimSpace(parts[2]))
					}
					if len(parts) > 4 {
						if d, err := strconv.ParseFloat(strings.TrimSpace(parts[4]), 64); err == nil {
							delay = time.Duration(d * float64(time.Millisecond))
						}
					}
					if len(parts) > 6 {
						colo = strings.TrimSpace(parts[6])
						if colo == "N/A" {
							colo = ""
						}
					}
					pingData = append(pingData, utils.CloudflareIPData{
						PingData: &utils.PingData{
							IP:       &net.IPAddr{IP: ip},
							Sended:   sended,
							Received: received,
							Delay:    delay,
							Colo:     colo,
						},
					})
				}
			}
			if len(pingData) > 0 {
				return pingData
			}
		}
	}

	// 否则常规加载（纯 IP 列表文本、CIDR 或 -ip 参数）
	ips := loadIPRanges()
	for _, ip := range ips {
		pingData = append(pingData, utils.CloudflareIPData{
			PingData: &utils.PingData{
				IP:       ip,
				Sended:   0,
				Received: 0,
				Delay:    0,
				Colo:     "",
			},
		})
	}
	return pingData
}
