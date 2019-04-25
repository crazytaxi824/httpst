package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	TotalTransactionCount int     // 总请求次数
	SuccessTransactions   int     // 成功次数
	FailedTransactions    int     // 失败次数
	SuccessRate           float64 // 成功率
	TransactionRate       float64 // 平均每秒处理请求数 = 总请求次数/总耗时
	ElapsedTime           float64 // 总耗时
	LongestTransaction    float64 // 最长耗时
	ShortestTransaction   float64 // 最短耗时

	waitReq   sync.WaitGroup // 等待请求完成
	waitStats sync.WaitGroup // 等待监听服务完成
	showData  bool           // 是否显示返回数据
)

func main() {
	users := flag.Uint("c", 1, "并发次数")
	urlReq := flag.String("r", "", "请求路径 eg:http://www.baidu.com")
	header := flag.String("H", "", "header, eg: token:a.b.c&Content-Type:application/x-www-form-urlencoded")
	method := flag.String("m", "get", "请求方法：get/post...")
	body := flag.String("P", "", "param, eg: id=1&name=abc")
	sd := flag.Bool("s", false, "是否显示返回数据 (default false)")
	flag.Parse()

	// 测试用 ------------------
	//*header = "x-user-token:eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJvbkRUWDVYdzRmaU9OYUExQTBKNUZiRGNCeXo0IiwidW5pcXVlTmFtZSI6Im9uRFRYNVh3NGZpT05hQTFBMEo1RmJEY0J5ejQiLCJpZCI6IjExMTg4MTIxMjQ2MDk1MTU1MjAiLCJuYW1lIjoiTVJIIiwiZXhwIjoxNTU2MTA3NzkxfQ.XsmiYyS1ualsT6QyTdR6uSUPK5A3DbgLz9DJFLQKt6CMCmlw1pIr_uVLSs0IscdftvQK6l7Pw1PlTTuWiYVVzRrNROlvrtfdPtn_4l1JUVZMSFBOyx2uE7_BlHzCmStGrRG_x2PgG8rHbbpO-XXphVhN6Ln3lPnZhi5aARIWxAY"
	//*body = "id=1&name=lq"
	//*method = "get"

	//*urlReq = "http://www.baidu.com"
	//*urlReq = "http://test.koudejun.com/api/1/v1/item/u/item/list?page=1&pageSize=6"
	//*urlReq = "http://127.0.0.1:18080/test"
	//*urlReq := "http://192.168.2.250:20004/item/u/item/list?page=1&pageSize=6"
	//*urlReq := "http://192.168.2.116:20086/item/u/item/list?page=1&pageSize=6"
	//-------------------

	// 判断并发次数
	if *users < uint(1) {
		fmt.Println("并发次数不能小于1")
		return
	}

	if *urlReq == "" {
		fmt.Println("请求路径不能为空")
		return
	}

	showData = *sd

	// 处理请求数据
	var headerKVSlice [][2]string
	if *header != "" {
		headerSlice := strings.Split(*header, "&")

		for _, v := range headerSlice {
			kv := strings.SplitN(v, ":", 2)
			if len(kv) != 2 {
				fmt.Println("header格式错误")
				return
			}

			var kvArray [2]string
			kvArray[0] = kv[0]
			kvArray[1] = kv[1]
			headerKVSlice = append(headerKVSlice, kvArray)
		}
	} else {
		headerKVSlice = nil
	}

	// -----------------

	maxMinValue := make(chan float64)
	addTxCount := make(chan bool)

	waitStats.Add(2)
	go replaceMaxMinValue(maxMinValue)
	go addTransactionStats(addTxCount)

	startTime := time.Now()

	var i uint = 0
	for i < *users {
		waitReq.Add(1)
		// 发送请求
		go httpGet(*urlReq, headerKVSlice, *method, *body, maxMinValue, addTxCount)
		i++
	}

	waitReq.Wait()

	// 统计总耗时
	endTime := time.Now()

	// 关闭chan
	close(maxMinValue)
	close(addTxCount)

	// 等待监听服务退出
	waitStats.Wait()

	// 总耗时
	ElapsedTime = endTime.Sub(startTime).Seconds()
	// 成功率
	SuccessRate = float64(SuccessTransactions) / float64(TotalTransactionCount) * 100

	// 每秒处理次数
	TransactionRate = float64(TotalTransactionCount) / ElapsedTime

	fmt.Println()
	fmt.Println("总请求数量:          ", TotalTransactionCount, "次")
	fmt.Println("成功请求数量: 	     ", SuccessTransactions, "次")
	fmt.Println("失败请求数量: 	     ", FailedTransactions, "次")
	fmt.Println("请求成功率:          ", fmt.Sprintf("%.2f", SuccessRate)+" %")
	fmt.Println("平均每秒处理请求数:  ", fmt.Sprintf("%.2f", TransactionRate)+" 次/秒")
	fmt.Println("最长耗时:     	     ", fmt.Sprintf("%.3f", LongestTransaction)+" 秒")
	fmt.Println("最短耗时:     	     ", fmt.Sprintf("%.3f", ShortestTransaction)+" 秒")
	fmt.Println("总耗时:       	     ", fmt.Sprintf("%.3f", ElapsedTime)+" 秒")
}

func httpGet(urlReq string, headerKVSlice [][2]string, method string, body string, maxMinValue chan<- float64, addTxCount chan<- bool) {
	defer waitReq.Done()

	// 开始计时
	startTime := time.Now()

	// 开始请求
	client := http.Client{}
	m := strings.ToUpper(strings.TrimSpace(method))
	//req, err := http.NewRequest(m, urlReq, body)
	req, err := http.NewRequest(m, urlReq, strings.NewReader(body))
	if err != nil {
		fmt.Println(err)
		return
	}

	if m != "GET" {
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded;param=value")
	}

	// header
	for _, v := range headerKVSlice {
		req.Header.Add(v[0], v[1])
	}
	//req.Header.Add("x-user-token", "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJvbkRUWDVYdzRmaU9OYUExQTBKNUZiRGNCeXo0IiwidW5pcXVlTmFtZSI6Im9uRFRYNVh3NGZpT05hQTFBMEo1RmJEY0J5ejQiLCJpZCI6IjExMTg4MTIxMjQ2MDk1MTU1MjAiLCJuYW1lIjoiTVJIIiwiZXhwIjoxNTU2MTA3NzkxfQ.XsmiYyS1ualsT6QyTdR6uSUPK5A3DbgLz9DJFLQKt6CMCmlw1pIr_uVLSs0IscdftvQK6l7Pw1PlTTuWiYVVzRrNROlvrtfdPtn_4l1JUVZMSFBOyx2uE7_BlHzCmStGrRG_x2PgG8rHbbpO-XXphVhN6Ln3lPnZhi5aARIWxAY")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()

	// 结束计时
	endTime := time.Now()

	// 单个耗时
	diffTime := endTime.Sub(startTime).Seconds()

	// 是否显示返回数据
	if showData {
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Println(err)
			return
		}

		fmt.Println("response data: ", string(b))
	}

	// 打印结果
	if req.URL.RawQuery != "" {
		fmt.Println(req.Proto, "| code:", resp.StatusCode, "|", diffTime, "秒", "==>", req.Method, req.Host+req.URL.Path+"?"+req.URL.RawQuery)
	} else {
		fmt.Println(req.Proto, "| code:", resp.StatusCode, "|", diffTime, "秒", "==>", req.Method, req.Host+req.URL.Path)
	}

	// 统计成功和失败
	if resp.StatusCode == 200 {
		// 成功 + 1
		addTxCount <- true
	} else {
		// 失败 + 1
		addTxCount <- false
	}

	// 统计最长最短时间
	maxMinValue <- diffTime

	return
}

// 替换最大最小值
func replaceMaxMinValue(ch <-chan float64) {
	defer waitStats.Done()

	for {
		select {
		case v, ok := <-ch:
			if !ok {
				return
			}

			if v > LongestTransaction {
				LongestTransaction = v
			}

			if ShortestTransaction == 0 {
				ShortestTransaction = v
			} else if v < ShortestTransaction {
				ShortestTransaction = v
			}
		}
	}
}

func addTransactionStats(ch <-chan bool) {
	defer waitStats.Done()

	for {
		select {
		case v, ok := <-ch:
			if !ok {
				return
			}

			if v {
				SuccessTransactions += 1
				TotalTransactionCount += 1
			} else {
				FailedTransactions += 1
				TotalTransactionCount += 1
			}
		}
	}
}
