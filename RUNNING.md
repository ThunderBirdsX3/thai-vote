# วิธีรันระบบ

อธิบายการทำงานของระบบอยู่ที่ [README.md](README.md)

## ความต้องการ

- Docker และ Docker Compose (สำหรับรันแบบเต็มระบบ)
- สำหรับพัฒนา: Go 1.26+, Node 24+

## รันด้วย Docker (แนะนำ)

```bash
cp .env.example .env        # แล้วแก้ค่าลับในไฟล์ .env
docker compose up -d --build
```

สร้างบัญชี admin คนแรก (รหัสผ่านอย่างน้อย 10 ตัวอักษร)

```bash
printf 'รหัสผ่านของคุณ\n' | docker compose exec -T api /app/api create-admin admin "ชื่อที่แสดง"
```

เปิดใช้งาน
- ผู้โหวต: http://localhost:8080
- Admin: http://localhost:8080/admin/login

คำสั่งที่ใช้บ่อย

```bash
docker compose logs -f api     # ดู log ของ API
docker compose down            # หยุดระบบ (ข้อมูลยังอยู่)
docker compose down -v         # หยุดและลบข้อมูลทั้งหมด
```

## ตั้งค่า (`.env`)

| ตัวแปร | ความหมาย |
|---|---|
| `POSTGRES_PASSWORD` | รหัสผ่านฐานข้อมูล |
| `ID_PEPPER` | ค่าลับสำหรับ hash เลขบัตรประชาชน สร้างด้วย `openssl rand -hex 32` ถ้าเปลี่ยนภายหลัง ระบบจะจำผู้โหวตเดิมไม่ได้ |
| `APP_ORIGIN` | URL ที่ผู้ใช้เปิดในเบราว์เซอร์ ต้องตรงทุกตัวอักษร (ใช้ตรวจ CSRF) |
| `MOCK_THAIID` | `true` เปิด login จำลอง ใช้เฉพาะ demo |
| `COOKIE_SECURE` | `true` เมื่อใช้ HTTPS |

## รันแบบพัฒนา

เริ่มฐานข้อมูล

```bash
docker run -d --name tv-db -e POSTGRES_USER=thaivote -e POSTGRES_PASSWORD=thaivote -e POSTGRES_DB=thaivote -p 55432:5432 postgres:16-alpine
```

เริ่ม backend (migration รันเองตอนเริ่ม)

```bash
cd backend
DATABASE_URL='postgres://thaivote:thaivote@localhost:55432/thaivote?sslmode=disable' MOCK_THAIID=true go run .
```

สร้าง admin

```bash
cd backend
DATABASE_URL='postgres://thaivote:thaivote@localhost:55432/thaivote?sslmode=disable' go run . create-admin admin "ชื่อที่แสดง"
```

เริ่ม frontend (http://localhost:5173 โดย proxy `/api` ไปที่ :8080)

```bash
cd frontend
npm install
npm run dev
```

ตอนพัฒนา ค่าเริ่มต้น `APP_ORIGIN` คือ `http://localhost:5173` ตรงกับ Vite อยู่แล้ว

## ทดสอบ

Test ใช้ฐานข้อมูล `thaivote_test` ที่ `localhost:55432` เป็นค่าเริ่มต้น (เปลี่ยนได้ด้วย `TEST_DATABASE_URL`) และ **ล้างข้อมูลทุกตาราง** ห้ามชี้ไปที่ข้อมูลจริง

```bash
docker exec tv-db createdb -U thaivote thaivote_test
cd backend && go test ./...
cd ../frontend && npm run build      # รวมการตรวจชนิดข้อมูลด้วย vue-tsc
```

## ก่อนนำไปใช้งานจริง

- ให้บริการผ่าน HTTPS และตั้ง `COOKIE_SECURE=true`
- ตั้ง `ID_PEPPER` และ `POSTGRES_PASSWORD` เป็นค่าลับที่คาดเดาไม่ได้ และเก็บ `ID_PEPPER` ไว้อย่างปลอดภัย
- ตั้ง `APP_ORIGIN` เป็น URL สาธารณะจริง
- ตั้ง `MOCK_THAIID=false` และเชื่อม ThaiID จริงก่อนเปิดให้ผู้ใช้ทั่วไป
- สำรองฐานข้อมูลสม่ำเสมอ
- จด head hash ของ poll ที่ปิดแล้วไว้นอกระบบ
