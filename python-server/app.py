from flask import Flask, jsonify
from sqlalchemy import create_engine
import os

app = Flask(__name__)

# Database connection
database_url = os.getenv('DATABASE_URL', 'postgresql://aryon:aryon@localhost:5432/aryondb?sslmode=disable')
engine = create_engine(database_url)


@app.route('/tenants')
def get_users():
    try:
        with engine.connect() as conn:
            result = conn.execute('SELECT * FROM tenants')
            users = [dict(row) for row in result]
            return jsonify(users)
    except Exception as e:
        return jsonify({"error": str(e)}), 500


if __name__ == '__main__':
    app.run(host='0.0.0.0')