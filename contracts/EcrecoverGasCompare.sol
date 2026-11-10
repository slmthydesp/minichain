// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// 对比两种恢复签名地址的做法。
///
/// recoverByPrecompile 调用地址 0x01。Solidity 自带的 ecrecover 就是这一次调用。
/// recoverByBytecode 不调用任何预编译，用字节码做 secp256k1 点运算，再 keccak256 出地址。
///
/// Remix 里用下面这组 hello world 的签名。两个函数都应返回
/// 0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266。
///
///   hash 0x47173285a8d7341e5e972fc677286384f802f8ef42a5ec5f03bbfa254cb01fad
///   v    28
///   r    0x3ee428fe71186c050ed93ada46fe27f4d252144592f434c2cec7e77deb9f12f4
///   s    0x2cc055a7f178b6c762dc121cef1e7fc6a611ed1c175285c730ffae9a76b33f75
///
/// 第二个函数大约要几百万 gas。Remix 调用前把 gas limit 调到 30000000。
/// 返回值里的 gasUsed 是函数体内的消耗，不含调用选择器和打包返回值。
contract EcrecoverGasCompare {
    uint256 private constant P = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2F;
    uint256 private constant N = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141;
    uint256 private constant GX = 0x79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798;
    uint256 private constant GY = 0x483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8;

    // 雅可比坐标。(x, y, 0) 表示无穷远点。
    struct J {
        uint256 x;
        uint256 y;
        uint256 z;
    }

    function recoverByPrecompile(bytes32 hash, uint8 v, bytes32 r, bytes32 s)
        external
        view
        returns (address signer, uint256 gasUsed)
    {
        uint256 start = gasleft();
        signer = _precompile(hash, v, r, s);
        gasUsed = start - gasleft();
    }

    function recoverByBytecode(bytes32 hash, uint8 v, bytes32 r, bytes32 s)
        external
        view
        returns (address signer, uint256 gasUsed)
    {
        uint256 start = gasleft();
        signer = _bytecode(hash, v, r, s);
        gasUsed = start - gasleft();
    }

    /// 同一次调用里跑两边。Remix 的返回值里能直接看见两个 gasUsed。
    function compare(bytes32 hash, uint8 v, bytes32 r, bytes32 s)
        external
        view
        returns (address precompile, uint256 gasPrecompile, address bytecode, uint256 gasBytecode)
    {
        uint256 start = gasleft();
        precompile = _precompile(hash, v, r, s);
        gasPrecompile = start - gasleft();
        start = gasleft();
        bytecode = _bytecode(hash, v, r, s);
        gasBytecode = start - gasleft();
    }

    function _precompile(bytes32 hash, uint8 v, bytes32 r, bytes32 s) private view returns (address signer) {
        assembly {
            let ptr := mload(0x40)
            mstore(ptr, hash)
            mstore(add(ptr, 32), v)
            mstore(add(ptr, 64), r)
            mstore(add(ptr, 96), s)
            // 失败时 0x01 返回空，不写输出。先把输出区清掉，避免读到上面的 hash。
            let ok := staticcall(gas(), 1, ptr, 128, ptr, 32)
            if iszero(ok) { revert(0, 0) }
            if eq(returndatasize(), 32) { signer := mload(ptr) }
        }
    }

    function _bytecode(bytes32 hash, uint8 v, bytes32 r, bytes32 s) private pure returns (address) {
        require(v == 27 || v == 28, "v");
        uint256 rv = uint256(r);
        uint256 sv = uint256(s);
        require(rv != 0 && rv < N && sv != 0 && sv < N, "r or s");

        uint256 y = _decompress(rv, v - 27);
        uint256 z = uint256(hash) % N;
        uint256 rInv = _modexp(rv, N - 2, N);
        // Q = (-hash * rInv) * G + (s * rInv) * R
        uint256 u1 = mulmod(N - z, rInv, N);
        uint256 u2 = mulmod(sv, rInv, N);
        (uint256 qx, uint256 qy) = _shamir(u1, u2, rv, y);
        return address(uint160(uint256(keccak256(abi.encodePacked(qx, qy)))));
    }

    /// y^2 = x^3 + 7。p 模 4 余 3，平方根是 y2^((p+1)/4)。parity 0 取偶数 y，1 取奇数 y。
    function _decompress(uint256 x, uint256 parity) private pure returns (uint256 y) {
        uint256 y2 = addmod(mulmod(x, mulmod(x, x, P), P), 7, P);
        y = _modexp(y2, (P + 1) / 4, P);
        require(mulmod(y, y, P) == y2, "r is not a point");
        if ((y & 1) != parity) y = P - y;
    }

    function _shamir(uint256 u1, uint256 u2, uint256 rx, uint256 ry) private pure returns (uint256 x, uint256 y) {
        J memory p;
        for (uint256 i = 255; ; ) {
            p = _dbl(p);
            if ((u1 >> i) & 1 == 1) p = _madd(p, GX, GY);
            if ((u2 >> i) & 1 == 1) p = _madd(p, rx, ry);
            if (i == 0) break;
            unchecked {
                i--;
            }
        }
        require(p.z != 0, "infinity");
        uint256 invz = _modexp(p.z, P - 2, P);
        uint256 invz2 = mulmod(invz, invz, P);
        x = mulmod(p.x, invz2, P);
        y = mulmod(p.y, mulmod(invz2, invz, P), P);
    }

    function _dbl(J memory p) private pure returns (J memory) {
        if (p.z == 0 || p.y == 0) return J(0, 0, 0);
        uint256 a = mulmod(p.x, p.x, P);
        uint256 bb = mulmod(p.y, p.y, P);
        uint256 c = mulmod(bb, bb, P);
        uint256 xb = addmod(p.x, bb, P);
        uint256 d = mulmod(2, _sub(mulmod(xb, xb, P), addmod(a, c, P)), P);
        uint256 e = mulmod(3, a, P);
        uint256 f = mulmod(e, e, P);
        uint256 x3 = _sub(f, mulmod(2, d, P));
        uint256 y3 = _sub(mulmod(e, _sub(d, x3), P), mulmod(8, c, P));
        uint256 z3 = mulmod(2, mulmod(p.y, p.z, P), P);
        return J(x3, y3, z3);
    }

    function _madd(J memory p, uint256 x2, uint256 y2) private pure returns (J memory) {
        if (p.z == 0) return J(x2, y2, 1);
        uint256 z1z1 = mulmod(p.z, p.z, P);
        uint256 u2 = mulmod(x2, z1z1, P);
        uint256 s2 = mulmod(y2, mulmod(p.z, z1z1, P), P);
        uint256 h = _sub(u2, p.x);
        if (h == 0) {
            if (s2 == p.y) return _dbl(p);
            return J(0, 0, 0);
        }
        return _maddFinish(p, z1z1, h, s2);
    }

    function _maddFinish(J memory p, uint256 z1z1, uint256 h, uint256 s2) private pure returns (J memory) {
        uint256 hh = mulmod(h, h, P);
        uint256 i4 = mulmod(4, hh, P);
        uint256 j = mulmod(h, i4, P);
        uint256 rr = mulmod(2, _sub(s2, p.y), P);
        uint256 vv = mulmod(p.x, i4, P);
        uint256 x3 = _sub(mulmod(rr, rr, P), j);
        x3 = _sub(x3, mulmod(2, vv, P));
        uint256 y3 = mulmod(rr, _sub(vv, x3), P);
        y3 = _sub(y3, mulmod(p.y, mulmod(2, j, P), P));
        uint256 z3 = addmod(p.z, h, P);
        z3 = _sub(mulmod(z3, z3, P), z1z1);
        z3 = _sub(z3, hh);
        return J(x3, y3, z3);
    }

    function _sub(uint256 a, uint256 b) private pure returns (uint256) {
        return addmod(a, P - b, P);
    }

    function _modexp(uint256 base, uint256 exp, uint256 m) private pure returns (uint256 result) {
        result = 1;
        while (exp != 0) {
            if (exp & 1 == 1) result = mulmod(result, base, m);
            base = mulmod(base, base, m);
            exp >>= 1;
        }
    }
}
