import asyncio
import random
import aiohttp
import time

API_URLS = [
    {"method": "POST", "url": "http://localhost:10000/api/v1/hotels/search"},
    {"method": "POST", "url": "http://localhost:10000/api/v1/hotels/bookings"},
    {"method": "DELETE", "url": "http://localhost:10000/api/v1/hotels/bookings"},
]
CHAT_ID = "123456789"  # Replace with a valid chat_id
MESSAGE = "Hello from script"

RPS = 30  # Requests per second
DURATION_SECONDS = 30  # Total duration to run (adjust as needed)

number_of_searches = 0
number_of_bookings = 0
number_of_deletions = 0

# number of searches always > number of bookings > number of deletions
def get_random_url():
    if number_of_searches < 500:
        return API_URLS[0]
    elif number_of_bookings < 124:
        return API_URLS[1]
    elif number_of_deletions < 20:
        return API_URLS[2]
    else:
        return API_URLS[0]

async def send_message(session, i):
    global number_of_searches, number_of_bookings, number_of_deletions
    payload = {
        "chat_id": CHAT_ID,
        "text": f"{MESSAGE} #{i}"
    }
    headers = {
        "X-Connection-Code": "vuonglv"
    }
    random_url = get_random_url()
    async with session.request(random_url["method"], random_url["url"], json=payload, headers=headers) as response:
        resp_text = await response.text()
        print(f"Request #{i}: {response.status} - {resp_text}")
        if random_url["method"] == "POST":
            if random_url["url"] == "http://localhost:10000/api/v1/hotels/search":
                number_of_searches += 1
            elif random_url["url"] == "http://localhost:10000/api/v1/hotels/bookings":
                number_of_bookings += 1
        elif random_url["method"] == "DELETE":
            number_of_deletions += 1


async def run_load():
    async with aiohttp.ClientSession() as session:
        for second in range(DURATION_SECONDS):
            tasks = [send_message(session, second * RPS + i) for i in range(RPS)]
            await asyncio.gather(*tasks)
            await asyncio.sleep(1)  # Ensure we keep it at 10 RPS


if __name__ == "__main__":
    asyncio.run(run_load())
