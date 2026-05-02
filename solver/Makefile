.PHONY: install run test fixture

install:
	python3 -m venv venv
	venv/bin/pip install -r requirements.txt

run:
	venv/bin/uvicorn main:app --host 0.0.0.0 --port 8081 --reload

test:
	venv/bin/pytest test_scheduler.py -v

# Run OR-Tools on a hard-coded scenario (override SCENARIO=name).
# Examples: make fixture SCENARIO=heavy_week  |  make fixture SCENARIO=ten_clients
SCENARIO ?= simple_three_clients
fixture:
	venv/bin/python run_fixture.py --scenario $(SCENARIO)
