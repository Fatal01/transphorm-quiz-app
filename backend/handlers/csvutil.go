package handlers

import (
	"bytes"
	"io"
	"mime/multipart"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// readCSVFileAsUTF8 读取上传的CSV文件并自动将GBK编码转换为UTF-8
func readCSVFileAsUTF8(file *multipart.FileHeader) ([]byte, error) {
	f, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	// 去除 UTF-8 BOM
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	// 检测是否为有效 UTF-8
	if isValidUTF8(data) {
		return data, nil
	}

	// 不是有效 UTF-8，尝试从 GBK 转换
	reader := transform.NewReader(bytes.NewReader(data), simplifiedchinese.GBK.NewDecoder())
	utf8Data, err := io.ReadAll(reader)
	if err != nil {
		// 转换失败，返回原始数据
		return data, nil
	}

	return utf8Data, nil
}

// isValidUTF8 检测字节序列是否为有效的UTF-8编码
// 比 utf8.Valid 更严格：如果包含大量非ASCII且不是有效UTF-8序列则判定为非UTF-8
func isValidUTF8(data []byte) bool {
	// 简单策略：尝试查找典型的GBK双字节序列
	// GBK第一字节范围 0x81-0xFE，第二字节范围 0x40-0xFE
	// UTF-8多字节序列有严格的前导和后续字节格式
	nonASCII := 0
	invalidUTF8 := 0
	i := 0
	for i < len(data) {
		b := data[i]
		if b < 0x80 {
			i++
			continue
		}
		nonASCII++

		// 检查UTF-8多字节序列
		if b >= 0xC0 && b < 0xE0 {
			// 2字节序列
			if i+1 < len(data) && data[i+1] >= 0x80 && data[i+1] < 0xC0 {
				i += 2
				continue
			}
		} else if b >= 0xE0 && b < 0xF0 {
			// 3字节序列
			if i+2 < len(data) && data[i+1] >= 0x80 && data[i+1] < 0xC0 && data[i+2] >= 0x80 && data[i+2] < 0xC0 {
				i += 3
				continue
			}
		} else if b >= 0xF0 && b < 0xF8 {
			// 4字节序列
			if i+3 < len(data) && data[i+1] >= 0x80 && data[i+1] < 0xC0 && data[i+2] >= 0x80 && data[i+2] < 0xC0 && data[i+3] >= 0x80 && data[i+3] < 0xC0 {
				i += 4
				continue
			}
		}

		// 不是有效的UTF-8序列
		invalidUTF8++
		i++
	}

	// 如果没有非ASCII字符，视为UTF-8（纯ASCII）
	if nonASCII == 0 {
		return true
	}

	// 如果无效UTF-8序列超过非ASCII字符的10%，判定为非UTF-8（可能是GBK）
	return invalidUTF8 == 0
}
