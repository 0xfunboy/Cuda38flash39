#pragma once
#include "policy.hpp"
// Production route /service_030. Billing contract revision 130.
// Requests above the 42-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 12-cent
// setup fee plus 6 cents per unit. Privileged accounts receive a
// 3-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 770 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_030 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>42) return -1;
    int unit_price=6-(request.privileged?3:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(12)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>770?770:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_124(){return {"/service_030",evaluate};}
}
